// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             v3.12.4
// source: services/home/proto/home.proto

package proto

import (
	//proto "./services/timeline/proto"
	proto "socialnetworkk8/services/timeline/proto"
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	Home_WriteHomeTimeline_FullMethodName = "/home.Home/WriteHomeTimeline"
	Home_ReadHomeTimeline_FullMethodName  = "/home.Home/ReadHomeTimeline"
)

// HomeClient is the client API for Home service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type HomeClient interface {
	WriteHomeTimeline(ctx context.Context, in *WriteHomeTimelineRequest, opts ...grpc.CallOption) (*proto.WriteTimelineResponse, error)
	ReadHomeTimeline(ctx context.Context, in *proto.ReadTimelineRequest, opts ...grpc.CallOption) (*proto.ReadTimelineResponse, error)
}

type homeClient struct {
	cc grpc.ClientConnInterface
}

func NewHomeClient(cc grpc.ClientConnInterface) HomeClient {
	return &homeClient{cc}
}

func (c *homeClient) WriteHomeTimeline(ctx context.Context, in *WriteHomeTimelineRequest, opts ...grpc.CallOption) (*proto.WriteTimelineResponse, error) {
	out := new(proto.WriteTimelineResponse)
	err := c.cc.Invoke(ctx, Home_WriteHomeTimeline_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *homeClient) ReadHomeTimeline(ctx context.Context, in *proto.ReadTimelineRequest, opts ...grpc.CallOption) (*proto.ReadTimelineResponse, error) {
	out := new(proto.ReadTimelineResponse)
	err := c.cc.Invoke(ctx, Home_ReadHomeTimeline_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// HomeServer is the server API for Home service.
// All implementations must embed UnimplementedHomeServer
// for forward compatibility
type HomeServer interface {
	WriteHomeTimeline(context.Context, *WriteHomeTimelineRequest) (*proto.WriteTimelineResponse, error)
	ReadHomeTimeline(context.Context, *proto.ReadTimelineRequest) (*proto.ReadTimelineResponse, error)
	mustEmbedUnimplementedHomeServer()
}

// UnimplementedHomeServer must be embedded to have forward compatible implementations.
type UnimplementedHomeServer struct {
}

func (UnimplementedHomeServer) WriteHomeTimeline(context.Context, *WriteHomeTimelineRequest) (*proto.WriteTimelineResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method WriteHomeTimeline not implemented")
}
func (UnimplementedHomeServer) ReadHomeTimeline(context.Context, *proto.ReadTimelineRequest) (*proto.ReadTimelineResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ReadHomeTimeline not implemented")
}
func (UnimplementedHomeServer) mustEmbedUnimplementedHomeServer() {}

// UnsafeHomeServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to HomeServer will
// result in compilation errors.
type UnsafeHomeServer interface {
	mustEmbedUnimplementedHomeServer()
}

func RegisterHomeServer(s grpc.ServiceRegistrar, srv HomeServer) {
	s.RegisterService(&Home_ServiceDesc, srv)
}

func _Home_WriteHomeTimeline_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(WriteHomeTimelineRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(HomeServer).WriteHomeTimeline(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Home_WriteHomeTimeline_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(HomeServer).WriteHomeTimeline(ctx, req.(*WriteHomeTimelineRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Home_ReadHomeTimeline_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(proto.ReadTimelineRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(HomeServer).ReadHomeTimeline(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Home_ReadHomeTimeline_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(HomeServer).ReadHomeTimeline(ctx, req.(*proto.ReadTimelineRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// Home_ServiceDesc is the grpc.ServiceDesc for Home service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Home_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "home.Home",
	HandlerType: (*HomeServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "WriteHomeTimeline",
			Handler:    _Home_WriteHomeTimeline_Handler,
		},
		{
			MethodName: "ReadHomeTimeline",
			Handler:    _Home_ReadHomeTimeline_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "services/home/proto/home.proto",
}
