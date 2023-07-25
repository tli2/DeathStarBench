package timeline

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"strconv"
	"time"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"net"
	"net/http"
	"net/http/pprof"
	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"socialnetworkk8/registry"
	"socialnetworkk8/tune"
	"socialnetworkk8/services/cacheclnt"
	"socialnetworkk8/tls"
	"socialnetworkk8/services/post"
	"socialnetworkk8/dialer"
	"socialnetworkk8/services/timeline/proto"
	postpb "socialnetworkk8/services/post/proto"
	opentracing "github.com/opentracing/opentracing-go"
	"socialnetworkk8/tracing"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	TIMELINE_SRV_NAME = "srv-timeline"
	TIMELINE_QUERY_OK = "OK"
	TIMELINE_CACHE_PREFIX = "timeline_"
)

type TimelineSrv struct {
	proto.UnimplementedTimelineServer 
	uuid         string
	cachec       *cacheclnt.CacheClnt
	mongoCo      *mongo.Collection
	postc        postpb.PostStorageClient
	Registry     *registry.Client
	Tracer       opentracing.Tracer
	Port         int
	IpAddr       string
	wCounter     *tracing.Counter
	rCounter     *tracing.Counter
}

func MakeTimelineSrv() *TimelineSrv {
	tune.Init()
	log.Info().Msg("Reading config...")
	jsonFile, err := os.Open("config.json")
	if err != nil {
		log.Error().Msgf("Got error while reading config: %v", err)
	}
	defer jsonFile.Close()
	byteValue, _ := ioutil.ReadAll(jsonFile)
	var result map[string]string
	json.Unmarshal([]byte(byteValue), &result)
	log.Info().Msg("Successfull")

	serv_port, _ := strconv.Atoi(result["TimelinePort"])
	serv_ip := result["TimelineIP"]
	log.Info().Msgf("Read target port: %v", serv_port)
	log.Info().Msgf("Read consul address: %v", result["consulAddress"])
	log.Info().Msgf("Read jaeger address: %v", result["jaegerAddress"])
	var (
		jaegeraddr = flag.String("jaegeraddr", result["jaegerAddress"], "Jaeger address")
		consuladdr = flag.String("consuladdr", result["consulAddress"], "Consul address")
	)
	flag.Parse()

	log.Info().Msgf("Initializing jaeger [service name: %v | host: %v]...", "timeline", *jaegeraddr)
	tracer, err := tracing.Init("timeline", *jaegeraddr)
	if err != nil {
		log.Panic().Msgf("Got error while initializing jaeger agent: %v", err)
	}
	log.Info().Msg("Jaeger agent initialized")

	log.Info().Msgf("Initializing consul agent [host: %v]...", *consuladdr)
	registry, err := registry.NewClient(*consuladdr)
	if err != nil {
		log.Panic().Msgf("Got error while initializing consul agent: %v", err)
	}
	log.Info().Msg("Consul agent initialized")
	log.Info().Msg("Start cache and DB connections")
	cachec := cacheclnt.MakeCacheClnt() 

	mongoUrl := "mongodb://" + result["MongoAddress"]
	log.Info().Msgf("Read database URL: %v", mongoUrl)
	mongoClient, err := mongo.Connect(
		context.Background(), options.Client().ApplyURI(mongoUrl).SetMaxPoolSize(2048))
	if err != nil {
		log.Panic().Msg(err.Error())
	}
	collection := mongoClient.Database("socialnetwork").Collection("timeline")
	indexModel := mongo.IndexModel{Keys: bson.D{{"userid", 1}}}
	name, err := collection.Indexes().CreateOne(context.TODO(), indexModel)
	log.Info().Msgf("Name of index created: %v", name)
	log.Info().Msg("New mongo session successfull...")

	return &TimelineSrv{
		Port:         serv_port,
		IpAddr:       serv_ip,
		Tracer:       tracer,
		Registry:     registry,
		cachec:       cachec,
		mongoCo:      collection,
		wCounter:     tracing.MakeCounter("Write-Timeline"),
		rCounter:     tracing.MakeCounter("Read-Timeline"),
	}
}

// Run starts the server
func (tlsrv *TimelineSrv) Run() error {
	if tlsrv.Port == 0 {
		return fmt.Errorf("server port must be set")
	}

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Info().Msg("Initializing gRPC clients...")
	conn, err := dialer.Dial(
		post.POST_SRV_NAME,
		tlsrv.Registry.Client,
		dialer.WithTracer(tlsrv.Tracer))
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	tlsrv.postc = postpb.NewPostStorageClient(conn)

	log.Info().Msg("Initializing gRPC Server...")
	tlsrv.uuid = uuid.New().String()
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout: 120 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
		}),
		grpc.UnaryInterceptor(
			otgrpc.OpenTracingServerInterceptor(tlsrv.Tracer),
		),
	}
	if tlsopt := tls.GetServerOpt(); tlsopt != nil {
		opts = append(opts, tlsopt)
	}
	grpcSrv := grpc.NewServer(opts...)
	proto.RegisterTimelineServer(grpcSrv, tlsrv)

	// listener
	log.Info().Msg("Initializing request listener ...")
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", tlsrv.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	http.Handle("/pprof/cpu", http.HandlerFunc(pprof.Profile))
	go func() {
		log.Error().Msgf("Error ListenAndServe: %v", http.ListenAndServe(":5000", nil))
	}()
	err = tlsrv.Registry.Register(TIMELINE_SRV_NAME, tlsrv.uuid, tlsrv.IpAddr, tlsrv.Port)
	if err != nil {
		return fmt.Errorf("failed register: %v", err)
	}
	log.Info().Msg("Successfully registered in consul")
	return grpcSrv.Serve(lis)
}

func (tlsrv *TimelineSrv) WriteTimeline(
		ctx context.Context, req *proto.WriteTimelineRequest) (
		*proto.WriteTimelineResponse, error) {
	t0 := time.Now()
	defer tlsrv.wCounter.AddTimeSince(t0)
	res := &proto.WriteTimelineResponse{Ok: "No"}
	_, err := tlsrv.mongoCo.UpdateOne(
		context.TODO(), &bson.M{"userid": req.Userid}, 
		&bson.M{"$push": bson.M{"postids": req.Postid, "timestamps": req.Timestamp}},
		options.Update().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	res.Ok = TIMELINE_QUERY_OK
	key := TIMELINE_CACHE_PREFIX + strconv.FormatInt(req.Userid, 10)
	if !tlsrv.cachec.Delete(ctx, key) {
		log.Error().Msgf("cannot delete timeline of %v", key)
	}
	return res, nil
}

func (tlsrv *TimelineSrv) ReadTimeline(
		ctx context.Context, req *proto.ReadTimelineRequest) (
		*proto.ReadTimelineResponse, error) {
	t0 := time.Now()
	defer tlsrv.rCounter.AddTimeSince(t0)
	res := &proto.ReadTimelineResponse{Ok: "No"}
	timeline, err := tlsrv.getUserTimeline(ctx, req.Userid)
	if err != nil {
		return nil, err
	}
	if timeline == nil {
		res.Ok = "No timeline item"
		return res, nil
	}
	start, stop, nItems := req.Start, req.Stop, int32(len(timeline.Postids))
	if start >= int32(nItems) || start >= stop {
		res.Ok = fmt.Sprintf("Cannot process start=%v end=%v for %v items", start, stop, nItems)
		return res, nil
	}	
	if stop > nItems {
		stop = nItems
	}
	postids := make([]int64, stop-start)
	for i := start; i < stop; i++ {
		postids[i-start] = timeline.Postids[nItems-i-1]
	}
	readPostReq := &postpb.ReadPostsRequest{Postids: postids}
	readPostRes, err := tlsrv.postc.ReadPosts(ctx, readPostReq)
	if err != nil {
		return nil, err 
	}
	res.Ok = readPostRes.Ok
	res.Posts = readPostRes.Posts
	return res, nil
}

func (tlsrv *TimelineSrv) getUserTimeline(ctx context.Context, userid int64) (*Timeline, error) {
	key := TIMELINE_CACHE_PREFIX + strconv.FormatInt(userid, 10) 
	timeline := &Timeline{}
	if timelineItem, err := tlsrv.cachec.Get(ctx, key); err != nil {
		if err != memcache.ErrCacheMiss {
			return nil, err
		}
		log.Debug().Msgf("Timeline %v cache miss", key)
		err = tlsrv.mongoCo.FindOne(context.TODO(), &bson.M{"userid": userid}).Decode(&timeline)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return nil, nil
			}
			return nil, err
		} 
		log.Debug().Msgf("Found timeline %v in DB: %v", userid, timeline)
		encodedTimeline, err := json.Marshal(timeline)	
		if err != nil {
			log.Error().Msg(err.Error())
			return nil, err
		}
		tlsrv.cachec.Set(ctx, &memcache.Item{Key: key, Value: encodedTimeline})
	} else {
		log.Debug().Msgf("Found timeline %v in cache!", userid)
		json.Unmarshal(timelineItem.Value, timeline)
	}
	return timeline, nil
}

type Timeline struct {
	Userid     int64   `bson:userid`
	Postids    []int64 `bson:postids`
	Timestamps []int64 `bson:timestamps`
}

