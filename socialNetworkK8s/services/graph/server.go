package graph

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
	"socialnetworkk8/dialer"
	"socialnetworkk8/services/cacheclnt"
	"socialnetworkk8/tls"
	"socialnetworkk8/services/user"
	"socialnetworkk8/services/graph/proto"
	userpb "socialnetworkk8/services/user/proto"
	opentracing "github.com/opentracing/opentracing-go"
	"socialnetworkk8/tracing"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	GRAPH_SRV_NAME = "srv-graph"
	GRAPH_QUERY_OK = "OK"
	FOLLOWER_CACHE_PREFIX = "followers_"
	FOLLOWEE_CACHE_PREFIX = "followees_"
)

// Server implements the user service
type GraphSrv struct {
	proto.UnimplementedGraphServer 
	uuid         string
	cachec       *cacheclnt.CacheClnt
	mongoSess    *mgo.Session
	mongoFlwERCo *mgo.Collection
	mongoFlwEECo *mgo.Collection
	userc        userpb.UserClient
	Registry     *registry.Client
	Tracer       opentracing.Tracer
	Port         int
	IpAddr       string
	fCounter     *tracing.Counter
}

func MakeGraphSrv() *GraphSrv {
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

	serv_port, _ := strconv.Atoi(result["GraphPort"])
	serv_ip := result["GraphIP"]
	log.Info().Msgf("Read target port: %v", serv_port)
	log.Info().Msgf("Read consul address: %v", result["consulAddress"])
	log.Info().Msgf("Read jaeger address: %v", result["jaegerAddress"])
	var (
		jaegeraddr = flag.String("jaegeraddr", result["jaegerAddress"], "Jaeger address")
		consuladdr = flag.String("consuladdr", result["consulAddress"], "Consul address")
	)
	flag.Parse()

	log.Info().Msgf("Initializing jaeger [service name: %v | host: %v]...", "graph", *jaegeraddr)
	tracer, err := tracing.Init("graph", *jaegeraddr)
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
	followersCo := session.DB("socialnetwork").C("graph-follower")
	followersCo.EnsureIndexKey("userid")
	followeesCo := session.DB("socialnetwork").C("graph-followee")
	followeesCo.EnsureIndexKey("userid")
	log.Info().Msg("New session successfull.")
	return &GraphSrv{
		Port:         serv_port,
		IpAddr:       serv_ip,
		Tracer:       tracer,
		Registry:     registry,
		cachec:       cachec,
		mongoSess:    session,
		mongoFlwERCo: followersCo,
		mongoFlwEECo: followeesCo,
		fCounter:     tracing.MakeCounter("Get-Follower"),
	}
}

// Run starts the server
func (gsrv *GraphSrv) Run() error {
	if gsrv.Port == 0 {
		return fmt.Errorf("server port must be set")
	}

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Info().Msg("Initializing gRPC clients...")
	conn, err := dialer.Dial(
		user.USER_SRV_NAME,
		gsrv.Registry.Client,
		dialer.WithTracer(gsrv.Tracer))
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	gsrv.userc = userpb.NewUserClient(conn)

	log.Info().Msg("Initializing gRPC Server...")
	gsrv.uuid = uuid.New().String()
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout: 120 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
		}),
		grpc.UnaryInterceptor(
			otgrpc.OpenTracingServerInterceptor(gsrv.Tracer),
		),
	}
	if tlsopt := tls.GetServerOpt(); tlsopt != nil {
		opts = append(opts, tlsopt)
	}
	grpcSrv := grpc.NewServer(opts...)
	proto.RegisterGraphServer(grpcSrv, gsrv)

	// listener
	log.Info().Msg("Initializing request listener ...")
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", gsrv.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	http.Handle("/pprof/cpu", http.HandlerFunc(pprof.Profile))
	go func() {
		log.Error().Msgf("Error ListenAndServe: %v", http.ListenAndServe(":5000", nil))
	}()
	err = gsrv.Registry.Register(GRAPH_SRV_NAME, gsrv.uuid, gsrv.IpAddr, gsrv.Port)
	if err != nil {
		return fmt.Errorf("failed register: %v", err)
	}
	log.Info().Msg("Successfully registered in consul")
	return grpcSrv.Serve(lis)
}

func (gsrv *GraphSrv) GetFollowers(
		ctx context.Context, req *proto.GetFollowersRequest) (*proto.GraphGetResponse, error) {
	t0 := time.Now()
	defer gsrv.fCounter.AddTimeSince(t0)
	res := &proto.GraphGetResponse{}
	res.Ok = "No"
	res.Userids = make([]int64, 0)
	followers, err := gsrv.getFollowers(ctx, req.Followeeid)
	if err == nil {
		res.Userids = followers
		res.Ok = GRAPH_QUERY_OK
	}
	return res, nil
}

func (gsrv *GraphSrv) GetFollowees(
		ctx context.Context, req *proto.GetFolloweesRequest) (*proto.GraphGetResponse, error) {
	res := &proto.GraphGetResponse{}
	res.Ok = "No"
	res.Userids = make([]int64, 0)
	followees, err := gsrv.getFollowees(ctx, req.Followerid)
	if err == nil {
		res.Userids = followees
		res.Ok = GRAPH_QUERY_OK
	}
	return res, nil
}

func (gsrv *GraphSrv) Follow(
		ctx context.Context, req *proto.FollowRequest) (*proto.GraphUpdateResponse, error) {
	return gsrv.updateGraph(ctx, req.Followerid, req.Followeeid, true)
}

func (gsrv *GraphSrv) Unfollow(
		ctx context.Context, req *proto.UnfollowRequest) (*proto.GraphUpdateResponse, error) {
	return gsrv.updateGraph(ctx, req.Followerid, req.Followeeid, false)
}

func (gsrv *GraphSrv) FollowWithUname(
		ctx context.Context, req *proto.FollowWithUnameRequest) (*proto.GraphUpdateResponse, error) {
	return gsrv.updateGraphWithUname(ctx, req.Followeruname, req.Followeeuname, true)
}

func (gsrv *GraphSrv) UnfollowWithUname(
		ctx context.Context, req *proto.UnfollowWithUnameRequest)(*proto.GraphUpdateResponse, error){
	return gsrv.updateGraphWithUname(ctx, req.Followeruname, req.Followeeuname, false)
}

func (gsrv *GraphSrv) updateGraph(
		ctx context.Context, followerid, followeeid int64, isFollow bool) (
		*proto.GraphUpdateResponse, error) {
	res := &proto.GraphUpdateResponse{}
	res.Ok = "No"
	log.Debug().Msgf("Updating graph. %v follows %v; Add edge? %v", followerid, followeeid, isFollow)
	if followerid == followeeid {
		if isFollow {
			res.Ok = "Cannot follow self."
		} else {
			res.Ok = "Cannot unfollow self."
		}
		return res, nil
	}
	var err1, err2 error
	if isFollow {
		_, err1 = gsrv.mongoFlwERCo.Upsert(
			&bson.M{"userid": followeeid}, 
			&bson.M{"$addToSet": bson.M{"edges": followerid}})
		_, err2 = gsrv.mongoFlwEECo.Upsert(
			&bson.M{"userid": followerid}, 
			&bson.M{"$addToSet": bson.M{"edges": followeeid}})
	} else {
		err1 = gsrv.mongoFlwERCo.Update(
			&bson.M{"userid": followeeid}, 
			&bson.M{"$pull": bson.M{"edges": followerid}})
		err2 = gsrv.mongoFlwEECo.Update(
			&bson.M{"userid": followerid}, 
			&bson.M{"$pull": bson.M{"edges": followeeid}})
	}
	if err1 != nil || err2 != nil {
		return res, fmt.Errorf("error updating graph %v %v", err1, err2)
	}
	res.Ok = GRAPH_QUERY_OK
	gsrv.clearCache(ctx, followerid, followeeid)
	return res, nil
}

func (gsrv *GraphSrv) updateGraphWithUname(
		ctx context.Context, follwerUname, followeeUname string, isFollow bool) (
		*proto.GraphUpdateResponse, error) {
	userReq := &userpb.CheckUserRequest{Usernames: []string{follwerUname, followeeUname}}
	userRes, err := gsrv.userc.CheckUser(ctx, userReq)
	if err != nil {
		return nil, err
	} else if userRes.Ok != user.USER_QUERY_OK {
		log.Error().Msgf("Missing user id for %v %v: %v", follwerUname, followeeUname, userRes)
		return &proto.GraphUpdateResponse{Ok: "Follower or Followee does not exist"}, nil
	}
	followerid, followeeid := userRes.Userids[0], userRes.Userids[1]
	return gsrv.updateGraph(ctx, followerid, followeeid, isFollow)
}

func (gsrv *GraphSrv) clearCache(ctx context.Context, followerid, followeeid int64) {
	follower_key := FOLLOWER_CACHE_PREFIX + strconv.FormatInt(followeeid, 10)
	followee_key := FOLLOWEE_CACHE_PREFIX + strconv.FormatInt(followerid, 10)
	if !gsrv.cachec.Delete(ctx, follower_key) {
		log.Error().Msgf("cannot delete followers of %v", follower_key)
	}
	if !gsrv.cachec.Delete(ctx, followee_key) {
		log.Error().Msgf("cannot delete followees of %v", follower_key)
	}
}

// Define getFollowers and getFollowees explicitly for clarity
func (gsrv *GraphSrv) getFollowers(ctx context.Context, userid int64) ([]int64, error) {
	key := FOLLOWER_CACHE_PREFIX + strconv.FormatInt(userid, 10)
	flwERInfo := &EdgeInfo{}
	if followerItem, err := gsrv.cachec.Get(ctx, key); err != nil {
		if err != memcache.ErrCacheMiss {
			return nil, err
		}
		log.Debug().Msgf("FollowER %v cache miss", key)
		var edgeInfos []EdgeInfo
		if err = gsrv.mongoFlwERCo.Find(&bson.M{"userid": userid}).All(&edgeInfos); err != nil {
			return nil, err
		} 
		if len(edgeInfos) == 0 {
			return make([]int64, 0), nil
		}
		flwERInfo = &edgeInfos[0]
		log.Debug().Msgf("Found followERs for  %v in DB: %v", userid, flwERInfo)
		encodedFlwERInfo, err := json.Marshal(flwERInfo)	
		if err != nil {
			log.Fatal().Msg(err.Error())
			return nil, err
		}
		gsrv.cachec.Set(ctx, &memcache.Item{Key: key, Value: encodedFlwERInfo})
	} else {
		log.Debug().Msgf("Found followERs for %v in cache!", userid)
		json.Unmarshal(followerItem.Value, flwERInfo)
	}	
	return flwERInfo.Edges, nil
}

func (gsrv *GraphSrv) getFollowees(ctx context.Context, userid int64) ([]int64, error) {
	key := FOLLOWEE_CACHE_PREFIX + strconv.FormatInt(userid, 10)
	flwEEInfo := &EdgeInfo{}
	if followeeItem, err := gsrv.cachec.Get(ctx, key); err != nil {
		if err != memcache.ErrCacheMiss {
			return nil, err
		}
		log.Debug().Msgf("FollowEE %v cache miss", key)
		var edgeInfos []EdgeInfo
		if err = gsrv.mongoFlwEECo.Find(&bson.M{"userid": userid}).All(&edgeInfos); err != nil {
			return nil, err
		} 
		if len(edgeInfos) == 0 {
			return make([]int64, 0), nil
		}
		flwEEInfo = &edgeInfos[0]
		log.Debug().Msgf("Found followEEs for  %v in DB: %v", userid, flwEEInfo)
		encodedFlwEEInfo, err := json.Marshal(flwEEInfo)	
		if err != nil {
			log.Fatal().Msg(err.Error())
			return nil, err
		}
		gsrv.cachec.Set(ctx, &memcache.Item{Key: key, Value: encodedFlwEEInfo})
	} else {
		log.Debug().Msgf("Found followEEs for %v in cache!", userid)
		json.Unmarshal(followeeItem.Value, flwEEInfo)
	}	
	return flwEEInfo.Edges, nil
}

type EdgeInfo struct {
	Userid int64   `bson:userid`
	Edges  []int64 `bson:edges`
}
