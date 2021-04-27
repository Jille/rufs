// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// DiscoveryServiceClient is the client API for DiscoveryService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type DiscoveryServiceClient interface {
	// Register signs your TLS client certificate.
	Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error)
	Connect(ctx context.Context, in *ConnectRequest, opts ...grpc.CallOption) (DiscoveryService_ConnectClient, error)
	GetMyIP(ctx context.Context, in *GetMyIPRequest, opts ...grpc.CallOption) (*GetMyIPResponse, error)
	ResolveConflict(ctx context.Context, in *ResolveConflictRequest, opts ...grpc.CallOption) (*ResolveConflictResponse, error)
	Orchestrate(ctx context.Context, opts ...grpc.CallOption) (DiscoveryService_OrchestrateClient, error)
	PushMetrics(ctx context.Context, in *PushMetricsRequest, opts ...grpc.CallOption) (*PushMetricsResponse, error)
}

type discoveryServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewDiscoveryServiceClient(cc grpc.ClientConnInterface) DiscoveryServiceClient {
	return &discoveryServiceClient{cc}
}

func (c *discoveryServiceClient) Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error) {
	out := new(RegisterResponse)
	err := c.cc.Invoke(ctx, "/DiscoveryService/Register", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *discoveryServiceClient) Connect(ctx context.Context, in *ConnectRequest, opts ...grpc.CallOption) (DiscoveryService_ConnectClient, error) {
	stream, err := c.cc.NewStream(ctx, &DiscoveryService_ServiceDesc.Streams[0], "/DiscoveryService/Connect", opts...)
	if err != nil {
		return nil, err
	}
	x := &discoveryServiceConnectClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type DiscoveryService_ConnectClient interface {
	Recv() (*ConnectResponse, error)
	grpc.ClientStream
}

type discoveryServiceConnectClient struct {
	grpc.ClientStream
}

func (x *discoveryServiceConnectClient) Recv() (*ConnectResponse, error) {
	m := new(ConnectResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *discoveryServiceClient) GetMyIP(ctx context.Context, in *GetMyIPRequest, opts ...grpc.CallOption) (*GetMyIPResponse, error) {
	out := new(GetMyIPResponse)
	err := c.cc.Invoke(ctx, "/DiscoveryService/GetMyIP", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *discoveryServiceClient) ResolveConflict(ctx context.Context, in *ResolveConflictRequest, opts ...grpc.CallOption) (*ResolveConflictResponse, error) {
	out := new(ResolveConflictResponse)
	err := c.cc.Invoke(ctx, "/DiscoveryService/ResolveConflict", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *discoveryServiceClient) Orchestrate(ctx context.Context, opts ...grpc.CallOption) (DiscoveryService_OrchestrateClient, error) {
	stream, err := c.cc.NewStream(ctx, &DiscoveryService_ServiceDesc.Streams[1], "/DiscoveryService/Orchestrate", opts...)
	if err != nil {
		return nil, err
	}
	x := &discoveryServiceOrchestrateClient{stream}
	return x, nil
}

type DiscoveryService_OrchestrateClient interface {
	Send(*OrchestrateRequest) error
	Recv() (*OrchestrateResponse, error)
	grpc.ClientStream
}

type discoveryServiceOrchestrateClient struct {
	grpc.ClientStream
}

func (x *discoveryServiceOrchestrateClient) Send(m *OrchestrateRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *discoveryServiceOrchestrateClient) Recv() (*OrchestrateResponse, error) {
	m := new(OrchestrateResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *discoveryServiceClient) PushMetrics(ctx context.Context, in *PushMetricsRequest, opts ...grpc.CallOption) (*PushMetricsResponse, error) {
	out := new(PushMetricsResponse)
	err := c.cc.Invoke(ctx, "/DiscoveryService/PushMetrics", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DiscoveryServiceServer is the server API for DiscoveryService service.
// All implementations must embed UnimplementedDiscoveryServiceServer
// for forward compatibility
type DiscoveryServiceServer interface {
	// Register signs your TLS client certificate.
	Register(context.Context, *RegisterRequest) (*RegisterResponse, error)
	Connect(*ConnectRequest, DiscoveryService_ConnectServer) error
	GetMyIP(context.Context, *GetMyIPRequest) (*GetMyIPResponse, error)
	ResolveConflict(context.Context, *ResolveConflictRequest) (*ResolveConflictResponse, error)
	Orchestrate(DiscoveryService_OrchestrateServer) error
	PushMetrics(context.Context, *PushMetricsRequest) (*PushMetricsResponse, error)
	mustEmbedUnimplementedDiscoveryServiceServer()
}

// UnimplementedDiscoveryServiceServer must be embedded to have forward compatible implementations.
type UnimplementedDiscoveryServiceServer struct {
}

func (UnimplementedDiscoveryServiceServer) Register(context.Context, *RegisterRequest) (*RegisterResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Register not implemented")
}
func (UnimplementedDiscoveryServiceServer) Connect(*ConnectRequest, DiscoveryService_ConnectServer) error {
	return status.Errorf(codes.Unimplemented, "method Connect not implemented")
}
func (UnimplementedDiscoveryServiceServer) GetMyIP(context.Context, *GetMyIPRequest) (*GetMyIPResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetMyIP not implemented")
}
func (UnimplementedDiscoveryServiceServer) ResolveConflict(context.Context, *ResolveConflictRequest) (*ResolveConflictResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ResolveConflict not implemented")
}
func (UnimplementedDiscoveryServiceServer) Orchestrate(DiscoveryService_OrchestrateServer) error {
	return status.Errorf(codes.Unimplemented, "method Orchestrate not implemented")
}
func (UnimplementedDiscoveryServiceServer) PushMetrics(context.Context, *PushMetricsRequest) (*PushMetricsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method PushMetrics not implemented")
}
func (UnimplementedDiscoveryServiceServer) mustEmbedUnimplementedDiscoveryServiceServer() {}

// UnsafeDiscoveryServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to DiscoveryServiceServer will
// result in compilation errors.
type UnsafeDiscoveryServiceServer interface {
	mustEmbedUnimplementedDiscoveryServiceServer()
}

func RegisterDiscoveryServiceServer(s grpc.ServiceRegistrar, srv DiscoveryServiceServer) {
	s.RegisterService(&DiscoveryService_ServiceDesc, srv)
}

func _DiscoveryService_Register_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RegisterRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DiscoveryServiceServer).Register(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/DiscoveryService/Register",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DiscoveryServiceServer).Register(ctx, req.(*RegisterRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DiscoveryService_Connect_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(ConnectRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(DiscoveryServiceServer).Connect(m, &discoveryServiceConnectServer{stream})
}

type DiscoveryService_ConnectServer interface {
	Send(*ConnectResponse) error
	grpc.ServerStream
}

type discoveryServiceConnectServer struct {
	grpc.ServerStream
}

func (x *discoveryServiceConnectServer) Send(m *ConnectResponse) error {
	return x.ServerStream.SendMsg(m)
}

func _DiscoveryService_GetMyIP_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetMyIPRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DiscoveryServiceServer).GetMyIP(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/DiscoveryService/GetMyIP",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DiscoveryServiceServer).GetMyIP(ctx, req.(*GetMyIPRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DiscoveryService_ResolveConflict_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ResolveConflictRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DiscoveryServiceServer).ResolveConflict(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/DiscoveryService/ResolveConflict",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DiscoveryServiceServer).ResolveConflict(ctx, req.(*ResolveConflictRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DiscoveryService_Orchestrate_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(DiscoveryServiceServer).Orchestrate(&discoveryServiceOrchestrateServer{stream})
}

type DiscoveryService_OrchestrateServer interface {
	Send(*OrchestrateResponse) error
	Recv() (*OrchestrateRequest, error)
	grpc.ServerStream
}

type discoveryServiceOrchestrateServer struct {
	grpc.ServerStream
}

func (x *discoveryServiceOrchestrateServer) Send(m *OrchestrateResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *discoveryServiceOrchestrateServer) Recv() (*OrchestrateRequest, error) {
	m := new(OrchestrateRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _DiscoveryService_PushMetrics_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PushMetricsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DiscoveryServiceServer).PushMetrics(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/DiscoveryService/PushMetrics",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DiscoveryServiceServer).PushMetrics(ctx, req.(*PushMetricsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// DiscoveryService_ServiceDesc is the grpc.ServiceDesc for DiscoveryService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var DiscoveryService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "DiscoveryService",
	HandlerType: (*DiscoveryServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Register",
			Handler:    _DiscoveryService_Register_Handler,
		},
		{
			MethodName: "GetMyIP",
			Handler:    _DiscoveryService_GetMyIP_Handler,
		},
		{
			MethodName: "ResolveConflict",
			Handler:    _DiscoveryService_ResolveConflict_Handler,
		},
		{
			MethodName: "PushMetrics",
			Handler:    _DiscoveryService_PushMetrics_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Connect",
			Handler:       _DiscoveryService_Connect_Handler,
			ServerStreams: true,
		},
		{
			StreamName:    "Orchestrate",
			Handler:       _DiscoveryService_Orchestrate_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "rufs.proto",
}

// ContentServiceClient is the client API for ContentService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type ContentServiceClient interface {
	ReadDir(ctx context.Context, in *ReadDirRequest, opts ...grpc.CallOption) (*ReadDirResponse, error)
	ReadFile(ctx context.Context, in *ReadFileRequest, opts ...grpc.CallOption) (ContentService_ReadFileClient, error)
	PassiveTransfer(ctx context.Context, opts ...grpc.CallOption) (ContentService_PassiveTransferClient, error)
}

type contentServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewContentServiceClient(cc grpc.ClientConnInterface) ContentServiceClient {
	return &contentServiceClient{cc}
}

func (c *contentServiceClient) ReadDir(ctx context.Context, in *ReadDirRequest, opts ...grpc.CallOption) (*ReadDirResponse, error) {
	out := new(ReadDirResponse)
	err := c.cc.Invoke(ctx, "/ContentService/ReadDir", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *contentServiceClient) ReadFile(ctx context.Context, in *ReadFileRequest, opts ...grpc.CallOption) (ContentService_ReadFileClient, error) {
	stream, err := c.cc.NewStream(ctx, &ContentService_ServiceDesc.Streams[0], "/ContentService/ReadFile", opts...)
	if err != nil {
		return nil, err
	}
	x := &contentServiceReadFileClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type ContentService_ReadFileClient interface {
	Recv() (*ReadFileResponse, error)
	grpc.ClientStream
}

type contentServiceReadFileClient struct {
	grpc.ClientStream
}

func (x *contentServiceReadFileClient) Recv() (*ReadFileResponse, error) {
	m := new(ReadFileResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *contentServiceClient) PassiveTransfer(ctx context.Context, opts ...grpc.CallOption) (ContentService_PassiveTransferClient, error) {
	stream, err := c.cc.NewStream(ctx, &ContentService_ServiceDesc.Streams[1], "/ContentService/PassiveTransfer", opts...)
	if err != nil {
		return nil, err
	}
	x := &contentServicePassiveTransferClient{stream}
	return x, nil
}

type ContentService_PassiveTransferClient interface {
	Send(*PassiveTransferData) error
	Recv() (*PassiveTransferData, error)
	grpc.ClientStream
}

type contentServicePassiveTransferClient struct {
	grpc.ClientStream
}

func (x *contentServicePassiveTransferClient) Send(m *PassiveTransferData) error {
	return x.ClientStream.SendMsg(m)
}

func (x *contentServicePassiveTransferClient) Recv() (*PassiveTransferData, error) {
	m := new(PassiveTransferData)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// ContentServiceServer is the server API for ContentService service.
// All implementations must embed UnimplementedContentServiceServer
// for forward compatibility
type ContentServiceServer interface {
	ReadDir(context.Context, *ReadDirRequest) (*ReadDirResponse, error)
	ReadFile(*ReadFileRequest, ContentService_ReadFileServer) error
	PassiveTransfer(ContentService_PassiveTransferServer) error
	mustEmbedUnimplementedContentServiceServer()
}

// UnimplementedContentServiceServer must be embedded to have forward compatible implementations.
type UnimplementedContentServiceServer struct {
}

func (UnimplementedContentServiceServer) ReadDir(context.Context, *ReadDirRequest) (*ReadDirResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ReadDir not implemented")
}
func (UnimplementedContentServiceServer) ReadFile(*ReadFileRequest, ContentService_ReadFileServer) error {
	return status.Errorf(codes.Unimplemented, "method ReadFile not implemented")
}
func (UnimplementedContentServiceServer) PassiveTransfer(ContentService_PassiveTransferServer) error {
	return status.Errorf(codes.Unimplemented, "method PassiveTransfer not implemented")
}
func (UnimplementedContentServiceServer) mustEmbedUnimplementedContentServiceServer() {}

// UnsafeContentServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to ContentServiceServer will
// result in compilation errors.
type UnsafeContentServiceServer interface {
	mustEmbedUnimplementedContentServiceServer()
}

func RegisterContentServiceServer(s grpc.ServiceRegistrar, srv ContentServiceServer) {
	s.RegisterService(&ContentService_ServiceDesc, srv)
}

func _ContentService_ReadDir_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ReadDirRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ContentServiceServer).ReadDir(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/ContentService/ReadDir",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ContentServiceServer).ReadDir(ctx, req.(*ReadDirRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _ContentService_ReadFile_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(ReadFileRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(ContentServiceServer).ReadFile(m, &contentServiceReadFileServer{stream})
}

type ContentService_ReadFileServer interface {
	Send(*ReadFileResponse) error
	grpc.ServerStream
}

type contentServiceReadFileServer struct {
	grpc.ServerStream
}

func (x *contentServiceReadFileServer) Send(m *ReadFileResponse) error {
	return x.ServerStream.SendMsg(m)
}

func _ContentService_PassiveTransfer_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(ContentServiceServer).PassiveTransfer(&contentServicePassiveTransferServer{stream})
}

type ContentService_PassiveTransferServer interface {
	Send(*PassiveTransferData) error
	Recv() (*PassiveTransferData, error)
	grpc.ServerStream
}

type contentServicePassiveTransferServer struct {
	grpc.ServerStream
}

func (x *contentServicePassiveTransferServer) Send(m *PassiveTransferData) error {
	return x.ServerStream.SendMsg(m)
}

func (x *contentServicePassiveTransferServer) Recv() (*PassiveTransferData, error) {
	m := new(PassiveTransferData)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// ContentService_ServiceDesc is the grpc.ServiceDesc for ContentService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var ContentService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "ContentService",
	HandlerType: (*ContentServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ReadDir",
			Handler:    _ContentService_ReadDir_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "ReadFile",
			Handler:       _ContentService_ReadFile_Handler,
			ServerStreams: true,
		},
		{
			StreamName:    "PassiveTransfer",
			Handler:       _ContentService_PassiveTransfer_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "rufs.proto",
}
