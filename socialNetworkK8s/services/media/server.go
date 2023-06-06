package media

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"strconv"
	"time"
	"fmt"
	"sync"
	"gopkg.in/mgo.v2"
	"math/rand"
	"gopkg.in/mgo.v2/bson"
	"net"
	"net/http"
	"net/http/pprof"
	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"socialnetworkk8/registry"
	"socialnetworkk8/tune"
	"socialnetworkk8/services/cacheclnt"
	"socialnetworkk8/tls"
	"socialnetworkk8/services/media/proto"
	opentracing "github.com/opentracing/opentracing-go"
	"socialnetworkk8/tracing"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	MEDIA_SRV_NAME = "srv-media"
	MEDIA_QUERY_OK = "OK"
	MEDIA_CACHE_PREFIX = "media_"
)

type MediaSrv struct {
	proto.UnimplementedMediaStorageServer 
	uuid         string
	cachec       *cacheclnt.CacheClnt
	mongoSess    *mgo.Session
	mongoCo      *mgo.Collection
	Registry     *registry.Client
	Tracer       opentracing.Tracer
	Port         int
	IpAddr       string
	sid          int32 // sid is a random number between 0 and 2^30
	ucount       int32 //This server may overflow with over 2^31 medias
    mu           sync.Mutex
}

func MakeMediaSrv() *MediaSrv {
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

	serv_port, _ := strconv.Atoi(result["MediaPort"])
	serv_ip := result["MediaIP"]
	log.Info().Msgf("Read target port: %v", serv_port)
	log.Info().Msgf("Read consul address: %v", result["consulAddress"])
	log.Info().Msgf("Read jaeger address: %v", result["jaegerAddress"])
	var (
		jaegeraddr = flag.String("jaegeraddr", result["jaegerAddress"], "Jaeger address")
		consuladdr = flag.String("consuladdr", result["consulAddress"], "Consul address")
	)
	flag.Parse()

	log.Info().Msgf("Initializing jaeger [service name: %v | host: %v]...", "media", *jaegeraddr)
	tracer, err := tracing.Init("media", *jaegeraddr)
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
	mongoUrl := result["MongoAddress"]
	log.Info().Msgf("Read database URL: %v", mongoUrl)
	session, err := mgo.Dial(mongoUrl)
	if err != nil {
		log.Panic().Msg(err.Error())
	}
	collection := session.DB("socialnetwork").C("media")
	collection.EnsureIndexKey("mediaid")
	log.Info().Msg("New session successfull.")
	return &MediaSrv{
		Port:         serv_port,
		IpAddr:       serv_ip,
		Tracer:       tracer,
		Registry:     registry,
		cachec:       cachec,
		mongoSess:    session,
		mongoCo:      collection,
	}
}

// Run starts the server
func (msrv *MediaSrv) Run() error {
	if msrv.Port == 0 {
		return fmt.Errorf("server port must be set")
	}

	//zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	log.Info().Msg("Initializing gRPC Server...")
	msrv.uuid = uuid.New().String()
	msrv.sid = rand.Int31n(536870912) // 2^29
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout: 120 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
		}),
		grpc.UnaryInterceptor(
			otgrpc.OpenTracingServerInterceptor(msrv.Tracer),
		),
	}
	if tlsopt := tls.GetServerOpt(); tlsopt != nil {
		opts = append(opts, tlsopt)
	}
	grpcSrv := grpc.NewServer(opts...)
	proto.RegisterMediaStorageServer(grpcSrv, msrv)

	// listener
	log.Info().Msg("Initializing request listener ...")
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", msrv.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	http.Handle("/pprof/cpu", http.HandlerFunc(pprof.Profile))
	go func() {
		log.Error().Msgf("Error ListenAndServe: %v", http.ListenAndServe(":5000", nil))
	}()
	err = msrv.Registry.Register(MEDIA_SRV_NAME, msrv.uuid, msrv.IpAddr, msrv.Port)
	if err != nil {
		return fmt.Errorf("failed register: %v", err)
	}
	log.Info().Msg("Successfully registered in consul")
	return grpcSrv.Serve(lis)
}

func (msrv *MediaSrv) StoreMedia(
		ctx context.Context, req *proto.StoreMediaRequest) (*proto.StoreMediaResponse, error){
	res := &proto.StoreMediaResponse{Ok: "No"}
	mId := msrv.getNextMediaId()
	media := &Media{mId, req.Mediatype, req.Mediadata}
	if err := msrv.mongoCo.Insert(media); err != nil {
		log.Fatal().Msg(err.Error())
		return res, err
	}
	res.Ok = MEDIA_QUERY_OK
	res.Mediaid = mId
	return res, nil
}

func (msrv *MediaSrv) ReadMedia(
		ctx context.Context, req *proto.ReadMediaRequest) (*proto.ReadMediaResponse, error){
	res := &proto.ReadMediaResponse{Ok: "No"}
	mediatypes := make([]string, len(req.Mediaids))
	mediadatas := make([][]byte, len(req.Mediaids))
	missing := false
	for idx, mediaid := range req.Mediaids {
		media, err := msrv.getMedia(ctx, mediaid)
		if err != nil {
			return nil, err
		} 
		if media == nil {
			missing = true
			res.Ok = res.Ok + fmt.Sprintf(" Missing %v.", mediaid)
		} else {
			mediatypes[idx] = media.Type
			mediadatas[idx] = media.Data
		}
	}
	res.Mediatypes = mediatypes
	res.Mediadatas = mediadatas
	if !missing {
		res.Ok = MEDIA_QUERY_OK
	}
	return res, nil
}

func (msrv *MediaSrv) getMedia(ctx context.Context, mediaid int64) (*Media, error) {
	key := MEDIA_CACHE_PREFIX + strconv.FormatInt(mediaid, 10) 
	media := &Media{}
	if mediaItem, err := msrv.cachec.Get(ctx, key); err != nil {
		if err != memcache.ErrCacheMiss {
			return nil, err
		}
		log.Info().Msgf("Media %v cache miss", key)
		var medias []Media
		if err = msrv.mongoCo.Find(&bson.M{"mediaid": mediaid}).All(&medias); err != nil {
			return nil, err
		} 
		if len(medias) == 0 {
			return nil, nil
		}
		media = &medias[0]
		log.Info().Msgf("Found media %v in DB: %v", mediaid, media)
		encodedMedia, err := json.Marshal(media)	
		if err != nil {
			log.Fatal().Msg(err.Error())
			return nil, err
		}
		msrv.cachec.Set(ctx, &memcache.Item{Key: key, Value: encodedMedia})
	} else {
		log.Info().Msgf("Found media %v in cache!", mediaid)
		json.Unmarshal(mediaItem.Value, media)
	}
	return media, nil
}

type Media struct {
	Mediaid int64  `bson:mediaid`
	Type    string `bson:type`
	Data    []byte `bson:data`
}

func (msrv *MediaSrv) incCountSafe() int32 {
	msrv.mu.Lock()
	defer msrv.mu.Unlock()
	msrv.ucount++
	return msrv.ucount
}

func (msrv *MediaSrv) getNextMediaId() int64 {
	return int64(msrv.sid)*1e10 + int64(msrv.incCountSafe())
}
