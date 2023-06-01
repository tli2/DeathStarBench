package post

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"strconv"
	"time"
	"fmt"
	"gopkg.in/mgo.v2"
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
	"socialnetworkk8/services/post/proto"
	opentracing "github.com/opentracing/opentracing-go"
	"socialnetworkk8/tracing"
	"github.com/rs/zerolog/log"
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
	mongoSess    *mgo.Session
	mongoCo      *mgo.Collection
	Registry     *registry.Client
	Tracer       opentracing.Tracer
	Port         int
	IpAddr       string
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
	mongoUrl := result["MongoAddress"]
	log.Info().Msgf("Read database URL: %v", mongoUrl)
	session, err := mgo.Dial(mongoUrl)
	if err != nil {
		log.Panic().Msg(err.Error())
	}
	collection := session.DB("socialnetwork").C("post")
	collection.EnsureIndexKey("postid")
	log.Info().Msg("New session successfull.")
	return &PostSrv{
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
func (psrv *PostSrv) Run() error {
	if psrv.Port == 0 {
		return fmt.Errorf("server port must be set")
	}

	//zerolog.SetGlobalLevel(zerolog.ErrorLevel)
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
	res := &proto.StorePostResponse{}
	res.Ok = "No"
	postBson := postToBson(req.Post)
	if err := psrv.mongoCo.Insert(postBson); err != nil {
		log.Fatal().Msg(err.Error())
		return res, err
	}
	return res, nil
}

func (psrv *PostSrv) ReadPosts(
		ctx context.Context, req *proto.ReadPostsRequest) (*proto.ReadPostsResponse, error) {
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
		log.Info().Msgf("Post %v cache miss", key)
		var postBsons []PostBson
		if err = psrv.mongoCo.Find(&bson.M{"postid": postid}).All(&postBsons); err != nil {
			return nil, err
		} 
		if len(postBsons) == 0 {
			return nil, nil
		}
		postBson = &postBsons[0]
		log.Info().Msgf("Found post %v in DB: %v", postid, postBson)
		encodedPost, err := json.Marshal(postBson)	
		if err != nil {
			log.Fatal().Msg(err.Error())
			return nil, err
		}
		psrv.cachec.Set(ctx, &memcache.Item{Key: key, Value: encodedPost})
	} else {
		log.Info().Msgf("Found post %v in cache!", postid)
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
		Text: post.Text,
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
		Text: bson.Text,
		Medias: bson.Medias,
		Urls: bson.Urls,
	}
}

type PostBson struct {
	Postid int64         `bson:postid`
	Posttype int32       `bson:posttype`
	Timestamp int64      `bson:timestamp`
	Creator int64        `bson:creator`
	Text string          `bson:text`
	Usermentions []int64 `bson:usermentions`
	Medias []int64       `bson:medias`
	Urls []string        `bson:urls`
}


