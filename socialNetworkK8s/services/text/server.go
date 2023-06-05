package text

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"strconv"
	"time"
	"regexp"
	"fmt"
	"sync"
	"net"
	"net/http"
	"net/http/pprof"
	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"socialnetworkk8/registry"
	"socialnetworkk8/tune"
	"socialnetworkk8/tls"
	"socialnetworkk8/dialer"
	"socialnetworkk8/services/user"
	"socialnetworkk8/services/url"
	"socialnetworkk8/services/text/proto"
	userpb "socialnetworkk8/services/user/proto"
	urlpb "socialnetworkk8/services/url/proto"
	opentracing "github.com/opentracing/opentracing-go"
	"socialnetworkk8/tracing"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	TEXT_SRV_NAME = "srv-text"
	TEXT_QUERY_OK = "OK"
)

var mentionRegex = regexp.MustCompile("@[a-zA-Z0-9-_]+") 
var urlRegex = regexp.MustCompile("(http://|https://)([a-zA-Z0-9_!~*'().&=+$%-/]+)")

type TextSrv struct {
	proto.UnimplementedTextServer 
	uuid         string
	Registry     *registry.Client
	userc        userpb.UserClient
	urlc         urlpb.UrlClient
	Tracer       opentracing.Tracer
	Port         int
	IpAddr       string
}

func MakeTextSrv() *TextSrv {
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

	serv_port, _ := strconv.Atoi(result["TextPort"])
	serv_ip := result["TextIP"]
	log.Info().Msgf("Read target port: %v", serv_port)
	log.Info().Msgf("Read consul address: %v", result["consulAddress"])
	log.Info().Msgf("Read jaeger address: %v", result["jaegerAddress"])
	var (
		jaegeraddr = flag.String("jaegeraddr", result["jaegerAddress"], "Jaeger address")
		consuladdr = flag.String("consuladdr", result["consulAddress"], "Consul address")
	)
	flag.Parse()

	log.Info().Msgf("Initializing jaeger [service name: %v | host: %v]...", "text", *jaegeraddr)
	tracer, err := tracing.Init("text", *jaegeraddr)
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
	return &TextSrv{
		Port:         serv_port,
		IpAddr:       serv_ip,
		Tracer:       tracer,
		Registry:     registry,
	}
}

// Run starts the server
func (tsrv *TextSrv) Run() error {
	if tsrv.Port == 0 {
		return fmt.Errorf("server port must be set")
	}

	//zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	log.Info().Msg("Initializing gRPC clients...")
	userConn, err := dialer.Dial(
		user.USER_SRV_NAME,
		tsrv.Registry.Client,
		dialer.WithTracer(tsrv.Tracer))
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	tsrv.userc = userpb.NewUserClient(userConn)
	urlConn, err := dialer.Dial(
		url.URL_SRV_NAME,
		tsrv.Registry.Client,
		dialer.WithTracer(tsrv.Tracer))
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	tsrv.urlc = urlpb.NewUrlClient(urlConn)

	log.Info().Msg("Initializing gRPC Server...")
	tsrv.uuid = uuid.New().String()
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout: 120 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
		}),
		grpc.UnaryInterceptor(
			otgrpc.OpenTracingServerInterceptor(tsrv.Tracer),
		),
	}
	if tlsopt := tls.GetServerOpt(); tlsopt != nil {
		opts = append(opts, tlsopt)
	}
	grpcSrv := grpc.NewServer(opts...)
	proto.RegisterTextServer(grpcSrv, tsrv)

	// listener
	log.Info().Msg("Initializing request listener ...")
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", tsrv.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	http.Handle("/pprof/cpu", http.HandlerFunc(pprof.Profile))
	go func() {
		log.Error().Msgf("Error ListenAndServe: %v", http.ListenAndServe(":5000", nil))
	}()
	err = tsrv.Registry.Register(TEXT_SRV_NAME, tsrv.uuid, tsrv.IpAddr, tsrv.Port)
	if err != nil {
		return fmt.Errorf("failed register: %v", err)
	}
	log.Info().Msg("Successfully registered in consul")
	return grpcSrv.Serve(lis)
}

func (tsrv *TextSrv) ProcessText(
		ctx context.Context, req *proto.ProcessTextRequest) (*proto.ProcessTextResponse, error) {
	res := &proto.ProcessTextResponse{}
	res.Ok = "No. "
	if req.Text == "" {
		res.Ok = "Cannot process empty text." 
		return res, nil
	}
	// find mentions and urls
	mentions := mentionRegex.FindAllString(req.Text, -1)
	mentionsL := len(mentions)
	usernames := make([]string, mentionsL)
	for idx, mention := range mentions {
		usernames[idx] = mention[1:]
	}
	userArg := &userpb.CheckUserRequest{Usernames: usernames}
	userRes := &userpb.CheckUserResponse{}

	urlIndices := urlRegex.FindAllStringIndex(req.Text, -1)
	urlIndicesL := len(urlIndices)
	extendedUrls := make([]string, urlIndicesL)
	for idx, loc := range urlIndices {
		extendedUrls[idx] = req.Text[loc[0]:loc[1]]
	}
	urlArg := &urlpb.ComposeUrlsRequest{Extendedurls: extendedUrls}
	urlRes := &urlpb.ComposeUrlsResponse{}

	// concurrent RPC calls
	var wg sync.WaitGroup
	var userErr, urlErr error
	if mentionsL > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			userRes, userErr = tsrv.userc.CheckUser(ctx, userArg)
		}()
	}
	if urlIndicesL > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			urlRes, urlErr = tsrv.urlc.ComposeUrls(ctx, urlArg)
		}()
	}
	wg.Wait()
	res.Text = req.Text
	if userErr != nil || urlErr != nil {
		return nil, fmt.Errorf("%w; %w", userErr, urlErr)
	} 

	// process mentions
	for idx, userid := range userRes.Userids {
		if userid > 0 {
			res.Usermentions = append(res.Usermentions, userid)
		} else {
			log.Info().Msgf("User %v does not exist!", usernames[idx])
		}
	}

	// process urls and text
	if urlIndicesL > 0 { 
		if urlRes.Ok != url.URL_QUERY_OK {
			log.Info().Msgf("cannot process urls %v!", extendedUrls)
			res.Ok += urlRes.Ok
			return res, nil
		} else {
			res.Urls = urlRes.Shorturls
			res.Text = ""
			prevLoc := 0
			for idx, loc := range urlIndices {
				res.Text += req.Text[prevLoc : loc[0]] + urlRes.Shorturls[idx]
				prevLoc = loc[1]
			}
			res.Text += req.Text[urlIndices[urlIndicesL-1][1]:]
		}
	}
	res.Ok = TEXT_QUERY_OK
	return res, nil
} 
