package content

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Jille/billy-grpc/fsserver"
	router "github.com/Jille/billy-router"
	"github.com/Jille/billy-router/emptyfs"
	"github.com/Jille/dfr"
	"github.com/Jille/rpcz"
	"github.com/Jille/rufs/client/connectivity"
	"github.com/Jille/rufs/client/metrics"
	"github.com/Jille/rufs/client/shares"
	"github.com/Jille/rufs/client/transfers"
	pb "github.com/Jille/rufs/proto"
	"github.com/Jille/rufs/security"
	"github.com/go-git/go-billy/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

var (
	mtx                  sync.Mutex
	circles              = map[string]*circleState{}
	swappableCredentials *security.SwappableCredentials
)

func Serve(addr string, kps []*security.KeyPair) error {
	if addr == "" {
		return errors.New("missing parameter addr")
	}

	swappableCredentials = security.NewSwappableCredentials(credentials.NewTLS(security.TLSConfigForServer(kps)))

	s := grpc.NewServer(
		grpc.Creds(swappableCredentials),
		grpc.ChainUnaryInterceptor(unaryInterceptor, rpcz.UnaryServerInterceptor),
		grpc.ChainStreamInterceptor(streamInterceptor, rpcz.StreamServerInterceptor),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             60 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     0,
			MaxConnectionAge:      0,
			MaxConnectionAgeGrace: 0,
			Time:                  60 * time.Second,
			Timeout:               30 * time.Second,
		}),
	)
	pb.RegisterContentServiceServer(s, content{})
	reflection.Register(s)
	if shares.HasAnyDirectIOShares() {
		fsserver.RegisterService(s, &directIO{
			cache: map[string]billy.Filesystem{},
		})
	}
	sock, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("content server failed to listen on %s: %v", addr, err)
	}
	log.Printf("content server listening on addr %s.", addr)
	go func() {
		if err := s.Serve(sock); err != nil {
			log.Fatalf("content server failed to serve on %s: %v", addr, err)
		}
	}()
	listener := &fakeListener{ch: make(chan net.Conn)}
	go func() {
		if err := s.Serve(listener); err != nil {
			log.Fatalf("content server failed to serve on fake listener: %v", err)
		}
	}()
	RegisterIncomingContentConnectionsServer(listener)
	return nil
}

func SwapKeyPairs(kps []*security.KeyPair) {
	swappableCredentials.Swap(credentials.NewTLS(security.TLSConfigForServer(kps)))
}

type content struct {
	pb.UnimplementedContentServiceServer
}

type circleState struct {
	mtx         sync.Mutex
	activeReads map[string]map[string]int
}

func unaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	peer, circle, err := security.PeerFromContext(ctx)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := handler(ctx, req)
	d := time.Since(start)
	metrics.AddContentRpcsRecv([]string{circle}, info.FullMethod, peer, status.Code(err).String(), 1)
	metrics.AppendContentRpcsRecvLatency([]string{circle}, info.FullMethod, peer, status.Code(err).String(), d.Seconds())
	return resp, err
}

func streamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	peer, circle, err := security.PeerFromContext(ss.Context())
	if err != nil {
		return err
	}
	start := time.Now()
	err = handler(srv, ss)
	d := time.Since(start)
	metrics.AddContentRpcsRecv([]string{circle}, info.FullMethod, peer, status.Code(err).String(), 1)
	metrics.AppendContentRpcsRecvLatency([]string{circle}, info.FullMethod, peer, status.Code(err).String(), d.Seconds())
	return err
}

func (content) ReadDir(ctx context.Context, req *pb.ReadDirRequest) (*pb.ReadDirResponse, error) {
	_, circle, err := security.PeerFromContext(ctx)
	if err != nil {
		return nil, err
	}

	res := &pb.ReadDirResponse{}
	res.Files, err = shares.Readdir(circle, req.GetPath())
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *circleState) increaseActiveCounter(path, peer string) {
	if _, found := s.activeReads[path]; !found {
		s.activeReads[path] = map[string]int{}
	}
	s.activeReads[path][peer]++
}

func (s *circleState) decreaseActiveCounter(path, peer string) {
	delete(s.activeReads[path], peer)
	if len(s.activeReads[path]) == 0 {
		delete(s.activeReads, path)
	}
}

func getCircleState(circle string) *circleState {
	mtx.Lock()
	defer mtx.Unlock()
	cs, ok := circles[circle]
	if !ok {
		cs = &circleState{
			activeReads: map[string]map[string]int{},
		}
		circles[circle] = cs
	}
	return cs
}

func (content) ReadFile(req *pb.ReadFileRequest, stream pb.ContentService_ReadFileServer) (retErr error) {
	d := dfr.D{}
	defer d.Run(&retErr)
	peer, circle, err := security.PeerFromContext(stream.Context())
	if err != nil {
		return err
	}

	fh, err := shares.Open(circle, req.GetFilename())
	if err != nil {
		return err
	}
	defer fh.Close()
	path := fh.Name()

	circleState := getCircleState(circle)

	circleState.mtx.Lock()
	circleState.increaseActiveCounter(path, peer)
	upgrade := len(circleState.activeReads[path]) > 1
	circleState.mtx.Unlock()
	defer func() {
		circleState.mtx.Lock()
		circleState.decreaseActiveCounter(path, peer)
		circleState.mtx.Unlock()
	}()

	if id, ok := transfers.IsLocalFileOrchestrated(circle, req.GetFilename()); ok {
		return stream.Send(&pb.ReadFileResponse{
			RedirectToOrchestratedDownload: id,
		})
	}

	if upgrade {
		id, err := transfers.SwitchToOrchestratedMode(circle, req.GetFilename())
		if err != nil {
			log.Printf("Failed to upgrade %q to orchestrated mode: %v", req.GetFilename(), err)
			metrics.AddContentOrchestrationJoinFailed([]string{circle}, "busy-file", 1)
		} else if id != 0 {
			return stream.Send(&pb.ReadFileResponse{
				RedirectToOrchestratedDownload: id,
			})
		}
	}

	var buf [8192]byte
	offset := req.GetOffset()
	remaining := req.GetRdnow()
	readNowDone := false
	for {
		for remaining <= 0 {
			if readNowDone {
				return nil
			}
			remaining = req.GetRdahead()
			readNowDone = true
		}
		r := remaining
		if r > int64(len(buf)) {
			r = int64(len(buf))
		}
		rn, err := fh.ReadAt(buf[:r], offset)
		if err != nil && err != io.EOF {
			return status.Errorf(codes.ResourceExhausted, "failed to read from %q at %d: %v", req.GetFilename(), offset, err)
		}
		n := int64(rn)
		if err := stream.Send(&pb.ReadFileResponse{
			Offset: offset,
			Data:   buf[:n],
		}); err != nil {
			return err
		}
		metrics.AddTransferSendBytes([]string{circle}, peer, "simple", n)
		if n < r {
			// Short read, so we hit EOF.
			return nil
		}
		offset += n
		remaining -= n
	}
}

func (content) PassiveTransfer(stream pb.ContentService_PassiveTransferServer) error {
	return transfers.HandleIncomingPassiveTransfer(stream)
}

type fakeListener struct {
	ch chan net.Conn
}

func (f *fakeListener) Accept() (net.Conn, error) {
	conn := <-f.ch
	return conn, nil
}

func (f *fakeListener) Close() error {
	return nil
}

func (f *fakeListener) Addr() net.Addr {
	return fakeAddr{}
}

type fakeAddr struct{}

func (fakeAddr) Network() string {
	return "fake"
}

func (fakeAddr) String() string {
	return "fake-address"
}

func RegisterIncomingContentConnectionsServer(l *fakeListener) {
	connectivity.HandleIncomingContentConnection = func(conn net.Conn) {
		l.ch <- conn
	}
}

type directIO struct {
	mtx   sync.Mutex
	cache map[string]billy.Filesystem
}

func (d *directIO) FilesystemForPeer(ctx context.Context) (billy.Filesystem, codes.Code, error) {
	peer, _, err := security.PeerFromContext(ctx)
	if err != nil {
		return nil, status.Code(err), err
	}
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if fs, found := d.cache[peer]; found {
		return fs, codes.OK, nil
	}
	granted := shares.SharesForPeer(peer)
	fs := router.New(emptyfs.New())
	for name, subfs := range granted {
		fs.Mount("/"+name, subfs)
	}
	d.cache[peer] = fs
	return fs, codes.OK, nil
}
