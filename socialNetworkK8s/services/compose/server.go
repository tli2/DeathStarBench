package compose

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"strconv"
	"time"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"net/http"
	"net/http/pprof"
	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"socialnetworkk8/registry"
	"socialnetworkk8/tune"
	"socialnetworkk8/services/compose/proto"
	"socialnetworkk8/services/text"
	textpb "socialnetworkk8/services/text/proto"
	"socialnetworkk8/services/post"
	postpb "socialnetworkk8/services/post/proto"
	"socialnetworkk8/services/timeline"
	tlpb "socialnetworkk8/services/timeline/proto"
	"socialnetworkk8/services/home"
	homepb "socialnetworkk8/services/home/proto"
	"socialnetworkk8/tls"
	"socialnetworkk8/dialer"
	opentracing "github.com/opentracing/opentracing-go"
	"socialnetworkk8/tracing"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	COMPOSE_SRV_NAME = "srv-compose"
	COMPOSE_QUERY_OK = "OK"
)

// Server implements the compose service
type ComposeSrv struct {
	proto.UnimplementedComposeServer
	uuid   		 string
	Registry     *registry.Client
	Tracer       opentracing.Tracer
	textc        textpb.TextClient
	postc        postpb.PostStorageClient
	tlc          tlpb.TimelineClient
	homec        homepb.HomeClient
	Port         int
	IpAddr       string
	sid          int32 // sid is a random number between 0 and 2^30
	pcount       int32 //This server may overflow with over 2^31 composes
    mu           sync.Mutex
	cCounter     *tracing.Counter
}

func MakeComposeSrv() *ComposeSrv {
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

	serv_port, _ := strconv.Atoi(result["ComposePort"])
	serv_ip := result["ComposeIP"]

	log.Info().Msgf("Read target port: %v", serv_port)
	log.Info().Msgf("Read consul address: %v", result["consulAddress"])
	log.Info().Msgf("Read jaeger address: %v", result["jaegerAddress"])
	var (
		jaegeraddr = flag.String("jaegeraddr", result["jaegerAddress"], "Jaeger address")
		consuladdr = flag.String("consuladdr", result["consulAddress"], "Consul address")
	)
	flag.Parse()

	log.Info().Msgf("Initializing jaeger [service name: %v | host: %v]...", "compose", *jaegeraddr)
	tracer, err := tracing.Init("compose", *jaegeraddr)
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
	return &ComposeSrv{
		Port:         serv_port,
		IpAddr:       serv_ip,
		Tracer:       tracer,
		Registry:     registry,
		cCounter:     tracing.MakeCounter("Compose-Post"),
	}
}

// Run starts the server
func (csrv *ComposeSrv) Run() error {
	if csrv.Port == 0 {
		return fmt.Errorf("server port must be set")
	}
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	log.Info().Msg("Initializing gRPC clients...")
	textConn, err := dialer.Dial(
		text.TEXT_SRV_NAME,
		csrv.Registry.Client,
		dialer.WithTracer(csrv.Tracer))
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	csrv.textc = textpb.NewTextClient(textConn)

	postConn, err := dialer.Dial(
		post.POST_SRV_NAME,
		csrv.Registry.Client,
		dialer.WithTracer(csrv.Tracer))
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	csrv.postc = postpb.NewPostStorageClient(postConn)

	tlConn, err := dialer.Dial(
		timeline.TIMELINE_SRV_NAME,
		csrv.Registry.Client,
		dialer.WithTracer(csrv.Tracer))
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	csrv.tlc = tlpb.NewTimelineClient(tlConn)

	homeConn, err := dialer.Dial(
		home.HOME_SRV_NAME,
		csrv.Registry.Client,
		dialer.WithTracer(csrv.Tracer))
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	csrv.homec = homepb.NewHomeClient(homeConn)
	csrv.uuid = uuid.New().String()
	csrv.sid = rand.Int31n(536870912) // 2^29
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout: 120 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
		}),
		grpc.UnaryInterceptor(
			otgrpc.OpenTracingServerInterceptor(csrv.Tracer),
		),
	}

	if tlsopt := tls.GetServerOpt(); tlsopt != nil {
		opts = append(opts, tlsopt)
	}

	grpcSrv := grpc.NewServer(opts...)

	proto.RegisterComposeServer(grpcSrv, csrv)

	// listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", csrv.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	http.Handle("/pprof/cpu", http.HandlerFunc(pprof.Profile))
	go func() {
		log.Error().Msgf("Error ListenAndServe: %v", http.ListenAndServe(":5000", nil))
	}()

	err = csrv.Registry.Register(COMPOSE_SRV_NAME, csrv.uuid, csrv.IpAddr, csrv.Port)
	if err != nil {
		return fmt.Errorf("failed register: %v", err)
	}
	log.Info().Msg("Successfully registered in consul")
	return grpcSrv.Serve(lis)
}

// Shutdown cleans up any processes
func (csrv *ComposeSrv) Shutdown() {
	csrv.Registry.Deregister(csrv.uuid)
}

func (csrv *ComposeSrv) ComposePost(
		ctx context.Context, req *proto.ComposePostRequest) (*proto.ComposePostResponse, error) {
	t0 := time.Now()
	defer csrv.cCounter.AddTimeSince(t0)
	log.Debug().Msgf("Recieved compose request: %v", req)
	res := &proto.ComposePostResponse{Ok: "No"}
	timestamp := time.Now().UnixNano()
	if req.Text == "" {
		res.Ok = "Cannot compose empty post!"
		return res, nil
	}
	// process text
	textReq := &textpb.ProcessTextRequest{Text: req.Text}
	textRes, err := csrv.textc.ProcessText(ctx, textReq)
	if err != nil {
		log.Error().Msgf("Error processing text: %v")
		return res, err
	}
	if textRes.Ok != text.TEXT_QUERY_OK {
		res.Ok += " Text Error: " + textRes.Ok
		return res, nil
	} 
	// create post
	newPost := &postpb.Post{
		Postid: csrv.getNextPostId(),
		Posttype: req.Posttype,
		Timestamp: timestamp,
		Creator: req.Userid,
		Creatoruname: req.Username,
		Text: textRes.Text,
		Usermentions: textRes.Usermentions,
		Urls: textRes.Urls,
		Medias: req.Mediaids,
	}
	log.Debug().Msgf("composing post: %v", newPost)
	
	// concurrently add post to storage and timelines
	var wg sync.WaitGroup
	var postErr, tlErr, homeErr error
	postReq := &postpb.StorePostRequest{Post: newPost}
	postRes := &postpb.StorePostResponse{}
	tlReq := &tlpb.WriteTimelineRequest{
		Userid: req.Userid, 
		Postid: newPost.Postid, 
		Timestamp: newPost.Timestamp}
	tlRes := &tlpb.WriteTimelineResponse{}
	homeReq := &homepb.WriteHomeTimelineRequest{
		Usermentionids: newPost.Usermentions, 
		Userid: req.Userid, 
		Postid: newPost.Postid, 
		Timestamp: newPost.Timestamp}
	homeRes := &tlpb.WriteTimelineResponse{}
	wg.Add(3)
	go func() {
		defer wg.Done()
		postRes, postErr = csrv.postc.StorePost(ctx, postReq)
	}()
	go func() {
		defer wg.Done()
		tlRes, tlErr = csrv.tlc.WriteTimeline(ctx, tlReq) 
	}()
	go func() {
		defer wg.Done()
		homeRes, homeErr = csrv.homec.WriteHomeTimeline(ctx, homeReq)
	}()
	wg.Wait()
	if postErr != nil || tlErr != nil || homeErr != nil {
		return nil, fmt.Errorf("%w; %w; %w", postErr, tlErr, homeErr)
	}
	if postRes.Ok != post.POST_QUERY_OK {
		res.Ok += " Post Error: " + postRes.Ok
		return res, nil
	} 
	if tlRes.Ok != timeline.TIMELINE_QUERY_OK {
		res.Ok += " Timeline Error: " + tlRes.Ok
		return res, nil
	}
	if homeRes.Ok != home.HOME_QUERY_OK {
		res.Ok += " Home Error: " + homeRes.Ok
		return res, nil
	}
	res.Ok = COMPOSE_QUERY_OK
	return res, nil
}

func (csrv *ComposeSrv) incCountSafe() int32 {
	csrv.mu.Lock()
	defer csrv.mu.Unlock()
	csrv.pcount++
	return csrv.pcount
}

func (csrv *ComposeSrv) getNextPostId() int64 {
	return int64(csrv.sid)*1e10 + int64(csrv.incCountSafe())
}
