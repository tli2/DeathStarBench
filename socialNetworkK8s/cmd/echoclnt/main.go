package main

import (
	"os"
	"time"
	"context"
	"socialnetworkk8/services/echo/proto"
	"socialnetworkk8/tracing"
	"socialnetworkk8/tune"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

func main() {
	ip := ""
	if len(os.Args) >= 2 {
		ip = os.Args[1]
	}
	tune.Init()
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).With().Timestamp().Caller().Logger()
	log.Info().Msg("Starting client...")
	dialopts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                1 * time.Hour,
			Timeout:             120 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithReadBufferSize(65536),
		grpc.WithWriteBufferSize(65536),
		grpc.WithInsecure(),
	}
	conn, err := grpc.Dial(ip + ":9001", dialopts...)
	if err != nil {
		log.Fatal().Msgf("Cannot dial grpc: %v", err)
		return 
	}
	echoc := proto.NewEchoClient(conn)
	counter := tracing.MakeCounter("echo-clnt")

	log.Info().Msg("Client started. Starting Experiments...")
	for i := 0; i < 1000; i++ {
		req := &proto.EchoRequest{Msg: "Hello!"} 
		t0 := time.Now()
		res, err := echoc.Echo(context.Background(), req)
		counter.AddTimeSince(t0)
		if err != nil {
			log.Fatal().Msgf("RPC Error: %v", err)
		} 
		if res.Echo != "Hello!" {
			log.Fatal().Msgf("Wrong echo: %v", res.Echo)
		}
	}
	log.Info().Msg("Experiments finished. Exiting...")
}
