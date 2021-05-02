package content

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Jille/dfr"
	"github.com/sgielen/rufs/client/metrics"
	"github.com/sgielen/rufs/client/shares"
	"github.com/sgielen/rufs/client/transfers"
	"github.com/sgielen/rufs/config"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

func New(addr string, kps []*security.KeyPair) (*content, error) {
	if addr == "" {
		return nil, errors.New("missing parameter addr")
	}

	c := &content{
		addr:     addr,
		keyPairs: kps,
		circles:  map[string]*circleState{},
	}

	for _, circle := range config.GetCircles() {
		c.circles[circle.Name] = &circleState{
			activeReads: map[string]int{},
		}
	}
	return c, nil
}

type content struct {
	pb.UnimplementedContentServiceServer

	addr     string
	keyPairs []*security.KeyPair
	circles  map[string]*circleState
}

type circleState struct {
	mtx         sync.Mutex
	activeReads map[string]int
}

func (c *content) Run() {
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(security.TLSConfigForServer(c.keyPairs))), grpc.ChainUnaryInterceptor(c.unaryInterceptor), grpc.ChainStreamInterceptor(c.streamInterceptor))
	pb.RegisterContentServiceServer(s, c)
	reflection.Register(s)
	sock, err := net.Listen("tcp", c.addr)
	if err != nil {
		log.Fatalf("content server failed to listen on %s: %v", c.addr, err)
	}
	log.Printf("content server listening on addr %s.", c.addr)
	if err := s.Serve(sock); err != nil {
		log.Fatalf("content server failed to serve on %s: %v", c.addr, err)
	}
}

func (c *content) unaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
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

func (c *content) streamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
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

func (c *content) ReadDir(ctx context.Context, req *pb.ReadDirRequest) (*pb.ReadDirResponse, error) {
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

func (c *content) ReadFile(req *pb.ReadFileRequest, stream pb.ContentService_ReadFileServer) (retErr error) {
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

	circleState := c.circles[circle]

	circleState.mtx.Lock()
	circleState.activeReads[path]++
	upgrade := circleState.activeReads[path] > 1
	circleState.mtx.Unlock()
	defer func() {
		circleState.mtx.Lock()
		circleState.activeReads[path]--
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

func (c *content) PassiveTransfer(stream pb.ContentService_PassiveTransferServer) error {
	return transfers.HandleIncomingPassiveTransfer(stream)
}
