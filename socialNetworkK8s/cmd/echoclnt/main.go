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
	"runtime/debug"
)

func main() {
	debug.SetGCPercent(-1)
	ip := ""
	if len(os.Args) >= 2 {
		ip = os.Args[1]
	}
	N_RPC_SESSION := 10
	tune.Init()
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).With().Timestamp().Caller().Logger()
	log.Info().Msg("Starting clients...")
	echocs := make([]proto.EchoClient, N_RPC_SESSION)
	req := &proto.EchoRequest{Msg: "Hello!"} 
	for i := range echocs {
			dialopts := []grpc.DialOption{
				grpc.WithKeepaliveParams(keepalive.ClientParameters{
					Time:                1 * time.Hour,
					Timeout:             120 * time.Second,
					PermitWithoutStream: true,
				}),
				grpc.WithReadBufferSize(65536000),
				grpc.WithWriteBufferSize(65536000),
				grpc.WithInsecure(),
			}
			conn, err := grpc.Dial(ip + ":9001", dialopts...)
			if err != nil {
				log.Fatal().Msgf("Cannot dial grpc: %v", err)
				return
			}
			echocs[i] = proto.NewEchoClient(conn)
			echocs[i].Echo(context.Background(), req)
	}
	counter := tracing.MakeCounter("echo-clnt")

	log.Info().Msg("Client started. Starting Experiments...")
	for i := 0; i < 5000; i++ {
		t0 := time.Now()
		res, err := echocs[i%N_RPC_SESSION].Echo(context.Background(), req)
		counter.AddTimeSince(t0)
		if err != nil {
			log.Fatal().Msgf("RPC Error: %v", err)
		} 
		if res.Echo != "Hello!" {
			log.Fatal().Msgf("Wrong echo: %v", res.Echo)
		}
		time.Sleep(2000 * time.Microsecond)
	}
	log.Info().Msg("Experiments finished. Exiting...")
}
