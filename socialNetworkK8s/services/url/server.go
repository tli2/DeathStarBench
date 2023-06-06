package url

import (
	"encoding/json"
	"math/rand"
	"flag"
	"io/ioutil"
	"os"
	"strconv"
	"time"
	"fmt"
	"strings"
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
	"socialnetworkk8/services/url/proto"
	opentracing "github.com/opentracing/opentracing-go"
	"socialnetworkk8/tracing"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	URL_SRV_NAME = "srv-url"
	URL_QUERY_OK = "OK"
	URL_CACHE_PREFIX = "url_"
	URL_HOSTNAME = "http://short-url/"
	URL_LENGTH = 10
)

var urlPrefixL = len(URL_HOSTNAME)
	
var letterRunes = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func init() {
    rand.Seed(time.Now().UnixNano())
}

func RandStringRunes(n int) string {
    b := make([]rune, n)
    for i := range b {
        b[i] = letterRunes[rand.Intn(len(letterRunes))]
    }
    return string(b)
}

type UrlSrv struct {
	proto.UnimplementedUrlServer 
	uuid         string
	cachec       *cacheclnt.CacheClnt
	mongoSess    *mgo.Session
	mongoCo      *mgo.Collection
	Registry     *registry.Client
	Tracer       opentracing.Tracer
	Port         int
	IpAddr       string
}

func MakeUrlSrv() *UrlSrv {
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

	serv_port, _ := strconv.Atoi(result["UrlPort"])
	serv_ip := result["UrlIP"]
	log.Info().Msgf("Read target port: %v", serv_port)
	log.Info().Msgf("Read consul address: %v", result["consulAddress"])
	log.Info().Msgf("Read jaeger address: %v", result["jaegerAddress"])
	var (
		jaegeraddr = flag.String("jaegeraddr", result["jaegerAddress"], "Jaeger address")
		consuladdr = flag.String("consuladdr", result["consulAddress"], "Consul address")
	)
	flag.Parse()

	log.Info().Msgf("Initializing jaeger [service name: %v | host: %v]...", "url", *jaegeraddr)
	tracer, err := tracing.Init("url", *jaegeraddr)
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
	collection := session.DB("socialnetwork").C("url")
	collection.EnsureIndexKey("shorturl")
	log.Info().Msg("New session successfull.")
	return &UrlSrv{
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
func (urlsrv *UrlSrv) Run() error {
	if urlsrv.Port == 0 {
		return fmt.Errorf("server port must be set")
	}

	//zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	log.Info().Msg("Initializing gRPC Server...")
	urlsrv.uuid = uuid.New().String()
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout: 120 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
		}),
		grpc.UnaryInterceptor(
			otgrpc.OpenTracingServerInterceptor(urlsrv.Tracer),
		),
	}
	if tlsopt := tls.GetServerOpt(); tlsopt != nil {
		opts = append(opts, tlsopt)
	}
	grpcSrv := grpc.NewServer(opts...)
	proto.RegisterUrlServer(grpcSrv, urlsrv)

	// listener
	log.Info().Msg("Initializing request listener ...")
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", urlsrv.Port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	http.Handle("/pprof/cpu", http.HandlerFunc(pprof.Profile))
	go func() {
		log.Error().Msgf("Error ListenAndServe: %v", http.ListenAndServe(":5000", nil))
	}()
	err = urlsrv.Registry.Register(URL_SRV_NAME, urlsrv.uuid, urlsrv.IpAddr, urlsrv.Port)
	if err != nil {
		return fmt.Errorf("failed register: %v", err)
	}
	log.Info().Msg("Successfully registered in consul")
	return grpcSrv.Serve(lis)
}


func (urlsrv *UrlSrv) ComposeUrls(
		ctx context.Context, req *proto.ComposeUrlsRequest) (*proto.ComposeUrlsResponse, error) {
	log.Info().Msgf("Received compose request %v", req)
	nUrls := len(req.Extendedurls)
	res := &proto.ComposeUrlsResponse{}
	if nUrls == 0 {
		res.Ok = "Empty input"
		return res, nil
	}
	res.Shorturls = make([]string, nUrls)
	for idx, extendedurl := range req.Extendedurls {
		shorturl := RandStringRunes(URL_LENGTH)
		url := &Url{Extendedurl: extendedurl, Shorturl: shorturl}
		if err := urlsrv.mongoCo.Insert(url); err != nil {
			log.Fatal().Msg(err.Error())
			return nil, err
		}
		res.Shorturls[idx] = URL_HOSTNAME + shorturl
	} 
	
	res.Ok = URL_QUERY_OK
	return res, nil
}

func (urlsrv *UrlSrv) GetUrls(
		ctx context.Context, req *proto.GetUrlsRequest) (*proto.GetUrlsResponse, error) {
	log.Info().Msgf("Received get request %v", req)
	res := &proto.GetUrlsResponse{}
	res.Ok = "No."
	extendedurls := make([]string, len(req.Shorturls))
	missing := false
	for idx, shorturl := range req.Shorturls {
		extendedurl, err := urlsrv.getExtendedUrl(ctx, shorturl)
		if err != nil {
			return nil, err
		} 
		if extendedurl == "" {
			missing = true
			res.Ok = res.Ok + fmt.Sprintf(" Missing %v.", shorturl)
		} else {
			extendedurls[idx] = extendedurl	
		}
	}
	res.Extendedurls = extendedurls
	if !missing {
		res.Ok = URL_QUERY_OK
	}
	return res, nil
}

func (urlsrv *UrlSrv) getExtendedUrl(ctx context.Context, shortUrl string) (string, error) {
	if !strings.HasPrefix(shortUrl, URL_HOSTNAME) {
		log.Info().Msgf("Url %v does not start with %v!", shortUrl, URL_HOSTNAME)
		return "", nil
	}
	urlKey := shortUrl[urlPrefixL:]
	key := URL_CACHE_PREFIX + urlKey
	url := &Url{}
	if urlItem, err := urlsrv.cachec.Get(ctx, key); err != nil {
		if err != memcache.ErrCacheMiss {
			return "", err
		}
		log.Info().Msgf("url %v cache miss", key)
		var urls []Url
		if err = urlsrv.mongoCo.Find(&bson.M{"shorturl": urlKey}).All(&urls); err != nil {
			return "", err
		} 
		if len(urls) == 0 {
			return "", nil
		}
		url = &urls[0]
		log.Info().Msgf("Found url %v in DB: %v", shortUrl, url)
		encodedUrl, err := json.Marshal(url)	
		if err != nil {
			log.Fatal().Msg(err.Error())
			return "", err
		}
		urlsrv.cachec.Set(ctx, &memcache.Item{Key: key, Value: encodedUrl})
	} else {
		log.Info().Msgf("Found url %v in cache!", key)
		json.Unmarshal(urlItem.Value, url)
	}
	return url.Extendedurl, nil
}


type Url struct {
	Shorturl string    `bson:shorturl`
	Extendedurl string `bson:extendedurl`
}
