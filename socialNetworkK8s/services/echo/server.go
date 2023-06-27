package echo

import (
	"time"
	"net"
	"socialnetworkk8/services/echo/proto"
	"socialnetworkk8/tracing"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// GRPC echo server without k8s contextx
type EchoSrv struct {
	proto.UnimplementedEchoServer
	counter *tracing.Counter
}

// Run starts the server
func RunEchoSrv() error {
	esrv := &EchoSrv{counter: tracing.MakeCounter("Echo")}
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout: 120 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			PermitWithoutStream: true,
		}),
	}
	grpcSrv := grpc.NewServer(opts...)
	proto.RegisterEchoServer(grpcSrv, esrv)
	log.Info().Msgf("Echo server registered")
	// listener
	lis, err := net.Listen("tcp", ":9001")
	if err != nil {
		log.Fatal().Msgf("failed to listen: %v", err)
		return err
	}
	log.Info().Msgf("Echo server started")
	return grpcSrv.Serve(lis)
}

func (esrv *EchoSrv) Echo(
		ctx context.Context, req *proto.EchoRequest) (*proto.EchoResponse, error) {
	t0 := time.Now()
	res := &proto.EchoResponse{Echo: req.Msg}
	esrv.counter.AddTimeSince(t0)
	return res, nil
}

