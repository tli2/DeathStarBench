package post

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
	"socialnetworkk8/services/post/proto"
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
	POST_SRV_NAME = "srv-post"
	POST_QUERY_OK = "OK"
	POST_CACHE_PREFIX = "post_"
)

type PostSrv struct {
	proto.UnimplementedPostStorageServer 
	uuid         string
	cachec       *cacheclnt.CacheClnt
	mongoCo      *mongo.Collection
	Registry     *registry.Client
	Tracer       opentracing.Tracer
	Port         int
	IpAddr       string
	sCounter     *tracing.Counter
	rCounter     *tracing.Counter
}

func MakePostSrv() *PostSrv {
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

	serv_port, _ := strconv.Atoi(result["PostPort"])
	serv_ip := result["PostIP"]
	log.Info().Msgf("Read target port: %v", serv_port)
	log.Info().Msgf("Read consul address: %v", result["consulAddress"])
	log.Info().Msgf("Read jaeger address: %v", result["jaegerAddress"])
	var (
		jaegeraddr = flag.String("jaegeraddr", result["jaegerAddress"], "Jaeger address")
		consuladdr = flag.String("consuladdr", result["consulAddress"], "Consul address")
	)
	flag.Parse()

	log.Info().Msgf("Initializing jaeger [service name: %v | host: %v]...", "post", *jaegeraddr)
	tracer, err := tracing.Init("post", *jaegeraddr)
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
	collection := mongoClient.Database("socialnetwork").Collection("post")
	indexModel := mongo.IndexModel{Keys: bson.D{{"postid", 1}}}
	name, err := collection.Indexes().CreateOne(context.TODO(), indexModel)
	log.Info().Msgf("Name of index created: %v", name)
	log.Info().Msg("New mongo session successfull...")

	return &PostSrv{
		Port:         serv_port,
		IpAddr:       serv_ip,
		Tracer:       tracer,
		Registry:     registry,
		cachec:       cachec,
		mongoCo:      collection,
		sCounter:     tracing.MakeCounter("Store-Post"),
		rCounter:     tracing.MakeCounter("Read-Post"),
	}
}

// Run starts the server
func (psrv *PostSrv) Run() error {
	if psrv.Port == 0 {
		return fmt.Errorf("server port must be set")
	}

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Info().Msg("Initializing gRPC Server...")
	psrv.uuid = uuid.New().String()
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout: 120 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
		}),
		grpc.UnaryInterceptor(
			otgrpc.OpenTracingServerInterceptor(psrv.Tracer),
		),
	}
	if tlsopt := tls.GetServerOpt(); tlsopt != nil {
		opts = append(opts, tlsopt)
	}
	grpcSrv := grpc.NewServer(opts...)
	proto.RegisterPostStorageServer(grpcSrv, psrv)

	// listener
	log.Info().Msg("Initializing request listener ...")
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", psrv.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	http.Handle("/pprof/cpu", http.HandlerFunc(pprof.Profile))
	go func() {
		log.Error().Msgf("Error ListenAndServe: %v", http.ListenAndServe(":5000", nil))
	}()
	err = psrv.Registry.Register(POST_SRV_NAME, psrv.uuid, psrv.IpAddr, psrv.Port)
	if err != nil {
		return fmt.Errorf("failed register: %v", err)
	}
	log.Info().Msg("Successfully registered in consul")
	return grpcSrv.Serve(lis)
}

func (psrv *PostSrv) StorePost(
		ctx context.Context, req *proto.StorePostRequest) (*proto.StorePostResponse, error) {
	t0 := time.Now()
	defer psrv.sCounter.AddTimeSince(t0)
	res := &proto.StorePostResponse{}
	res.Ok = "No"
	postBson := postToBson(req.Post)
	if _, err := psrv.mongoCo.InsertOne(context.TODO(), postBson); err != nil {
		log.Error().Msg(err.Error())
		return res, err
	}
	res.Ok = POST_QUERY_OK
	return res, nil
}

func (psrv *PostSrv) ReadPosts(
		ctx context.Context, req *proto.ReadPostsRequest) (*proto.ReadPostsResponse, error) {
	t0 := time.Now()
	defer psrv.rCounter.AddTimeSince(t0)
	res := &proto.ReadPostsResponse{}
	res.Ok = "No."
	posts := make([]*proto.Post, len(req.Postids))
	missing := false
	for idx, postid := range req.Postids {
		postBson, err := psrv.getPost(ctx, postid)
		if err != nil {
			return nil, err
		} 
		if postBson == nil {
			missing = true
			res.Ok = res.Ok + fmt.Sprintf(" Missing %v.", postid)
		} else {
			posts[idx] = bsonToPost(postBson)
		}
	}
	res.Posts = posts
	if !missing {
		res.Ok = POST_QUERY_OK
	}
	return res, nil
}

func (psrv *PostSrv) getPost(ctx context.Context, postid int64) (*PostBson, error) {
	key := POST_CACHE_PREFIX + strconv.FormatInt(postid, 10) 
	postBson := &PostBson{}
	if postItem, err := psrv.cachec.Get(ctx, key); err != nil {
		if err != memcache.ErrCacheMiss {
			return nil, err
		}
		log.Debug().Msgf("Post %v cache miss", key)
		err = psrv.mongoCo.FindOne(context.TODO(), &bson.M{"postid": postid}).Decode(&postBson)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return nil, nil
			}
			return nil, err
		} 
		log.Debug().Msgf("Found post %v in DB: %v", postid, postBson)
		encodedPost, err := json.Marshal(postBson)	
		if err != nil {
			log.Error().Msg(err.Error())
			return nil, err
		}
		psrv.cachec.Set(ctx, &memcache.Item{Key: key, Value: encodedPost})
	} else {
		log.Debug().Msgf("Found post %v in cache!", postid)
		json.Unmarshal(postItem.Value, postBson)
	}
	return postBson, nil
}

func postToBson(post *proto.Post) *PostBson {
	return &PostBson{
		Postid: post.Postid,
		Posttype: int32(post.Posttype),
		Timestamp: post.Timestamp,
		Creator: post.Creator,
		CreatorUname: post.Creatoruname,
		Text: post.Text,
		Usermentions: post.Usermentions,
		Medias: post.Medias,
		Urls: post.Urls,
	}
}

func bsonToPost(bson *PostBson) *proto.Post {
	return &proto.Post{
		Postid: bson.Postid,
		Posttype: proto.POST_TYPE(bson.Posttype),
		Timestamp: bson.Timestamp,
		Creator: bson.Creator,
		Creatoruname: bson.CreatorUname,
		Text: bson.Text,
		Usermentions: bson.Usermentions,
		Medias: bson.Medias,
		Urls: bson.Urls,
	}
}

type PostBson struct {
	Postid int64         `bson:postid`
	Posttype int32       `bson:posttype`
	Timestamp int64      `bson:timestamp`
	Creator int64        `bson:creator`
	CreatorUname string  `bson:creatoruname`
	Text string          `bson:text`
	Usermentions []int64 `bson:usermentions`
	Medias []int64       `bson:medias`
	Urls []string        `bson:urls`
}


