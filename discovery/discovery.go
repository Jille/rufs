package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/jrick/logrotate/rotator"
	"github.com/sgielen/rufs/discovery/metrics"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"github.com/sgielen/rufs/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	// Register /debug/ HTTP handlers.
	_ "github.com/sgielen/rufs/debugging"
)

var (
	port          = flag.Int("port", 12000, "gRPC port")
	certdir       = flag.String("certdir", "", "Where CA certs are read from (see create_ca_pair)")
	collectedLogs = flag.String("collected_logs_file", "", "Path to store collected logs in")

	mutexProfileFraction = flag.Int("mutex_profile_fraction", 0, "Controls the fraction of mutex contention events that are reported in the mutex profile. On average 1/rate events are reported.")
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile | log.Lmicroseconds)
	flag.Parse()
	runtime.SetMutexProfileFraction(*mutexProfileFraction)

	if *certdir == "" {
		log.Fatalf("Flag --certdir is required")
	}

	log.Printf("starting rufs %s", version.GetVersion())

	ca, err := security.LoadCAKeyPair(*certdir)
	if err != nil {
		log.Fatalf("Failed to load CA key pair: %v", err)
	}

	d := &discovery{
		ca:       ca,
		circle:   ca.Name(),
		clients:  map[string]*client{},
		rotators: map[string]*rotator.Rotator{},
	}
	d.cond = sync.NewCond(&d.mtx)
	go d.keepAliver()
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(ca.TLSConfigForDiscovery())))
	pb.RegisterDiscoveryServiceServer(s, d)
	reflection.Register(s)
	sock, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	log.Printf("Listening on port %d for circle %q", *port, d.circle)
	go RunStun(*port)
	go metrics.Serve()
	if err := s.Serve(sock); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

type discovery struct {
	pb.UnimplementedDiscoveryServiceServer

	ca     *security.CAKeyPair
	circle string

	mtx     sync.Mutex
	clients map[string]*client
	cond    *sync.Cond

	loggingMtx sync.Mutex
	rotators   map[string]*rotator.Rotator
}

type client struct {
	peer                    *pb.Peer
	stream                  pb.DiscoveryService_ConnectServer
	newPeerList             bool
	newActiveDownloads      bool
	resolveConflictRequests []*pb.ResolveConflictRequest
	nextKeepAlive           time.Time
}

func (d *discovery) broadcastNewActiveDownloads() {
	d.mtx.Lock()
	for _, dc := range d.clients {
		dc.newActiveDownloads = true
	}
	d.cond.Broadcast()
	d.mtx.Unlock()
}

func (d *discovery) keepAliver() {
	for range time.Tick(5 * time.Second) {
		d.cond.Broadcast()
	}
}

func (d *discovery) Connect(req *pb.ConnectRequest, stream pb.DiscoveryService_ConnectServer) error {
	name, _, err := security.PeerFromContext(stream.Context())
	if err != nil {
		return err
	}
	d.mtx.Lock()
	defer d.mtx.Unlock()
	c := &client{
		peer: &pb.Peer{
			Name:         name,
			Endpoints:    req.GetEndpoints(),
			UdpEndpoints: req.GetUdpEndpoints(),
		},
		stream:             stream,
		newPeerList:        true,
		newActiveDownloads: true,
	}
	d.clients[name] = c
	for _, c2 := range d.clients {
		c2.newPeerList = true
	}
	d.cond.Broadcast()
	defer func() {
		if d.clients[name] == c {
			delete(d.clients, name)
			for _, c2 := range d.clients {
				c2.newPeerList = true
			}
			d.cond.Broadcast()
		}
	}()

	for {
		now := time.Now()
		for c.newPeerList || c.newActiveDownloads || len(c.resolveConflictRequests) > 0 || c.nextKeepAlive.Before(now) {
			msg := &pb.ConnectResponse{}
			if len(c.resolveConflictRequests) > 0 {
				msg.Msg = &pb.ConnectResponse_ResolveConflictRequest{
					ResolveConflictRequest: c.resolveConflictRequests[0],
				}
				c.resolveConflictRequests = c.resolveConflictRequests[1:]
			} else if c.newActiveDownloads {
				activeOrchestrationMtx.Lock()
				active := make([]*pb.ConnectResponse_ActiveDownload, 0, len(activeOrchestration))
				for _, ao := range activeOrchestration {
					active = append(active, ao.activeDownload)
				}
				activeOrchestrationMtx.Unlock()
				msg.Msg = &pb.ConnectResponse_ActiveDownloads{
					ActiveDownloads: &pb.ConnectResponse_ActiveDownloadList{
						ActiveDownloads: active,
					},
				}
				c.newActiveDownloads = false
			} else if c.newPeerList {
				msg.Msg = &pb.ConnectResponse_PeerList_{
					PeerList: &pb.ConnectResponse_PeerList{},
				}
				for _, c2 := range d.clients {
					msg.GetPeerList().Peers = append(msg.GetPeerList().Peers, c2.peer)
				}
				c.newPeerList = false
			} else {
				// Send a keep alive message without any content.
			}
			c.nextKeepAlive = now.Add(5 * time.Second)
			d.mtx.Unlock()
			if err := stream.Send(msg); err != nil {
				d.mtx.Lock()
				return err
			}
			d.mtx.Lock()
			if d.clients[name] != c {
				return errors.New("connection from another process for your username")
			}
		}

		d.cond.Wait()
		if d.clients[name] != c {
			return errors.New("connection from another process for your username")
		}
	}
}

func (d *discovery) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	token := d.ca.CreateToken(req.GetUsername())
	if token != req.GetToken() {
		return nil, status.Error(codes.PermissionDenied, "token is incorrect")
	}
	cert, err := d.ca.Sign(req.GetPublicKey(), fmt.Sprintf("%s@%s", req.GetUsername(), d.circle))
	if err != nil {
		return nil, err
	}
	return &pb.RegisterResponse{
		Certificate: cert,
	}, nil
}

func (d *discovery) GetMyIP(ctx context.Context, req *pb.GetMyIPRequest) (*pb.GetMyIPResponse, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		// This should never happen.
		return nil, status.Error(codes.Internal, "no Peer attached to context")
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to extract IP from %q", p.Addr.String())
	}
	return &pb.GetMyIPResponse{
		Ip: host,
	}, nil
}

func (d *discovery) ResolveConflict(ctx context.Context, req *pb.ResolveConflictRequest) (*pb.ResolveConflictResponse, error) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	req = proto.Clone(req).(*pb.ResolveConflictRequest)
	for _, c := range d.clients {
		c.resolveConflictRequests = append(c.resolveConflictRequests, req)
	}
	d.cond.Broadcast()
	return &pb.ResolveConflictResponse{}, nil
}

func (d *discovery) PushMetrics(ctx context.Context, req *pb.PushMetricsRequest) (*pb.PushMetricsResponse, error) {
	if err := metrics.PushMetrics(ctx, req); err != nil {
		return nil, err
	}
	return &pb.PushMetricsResponse{}, nil
}

func (d *discovery) PushLogs(ctx context.Context, req *pb.PushLogsRequest) (*pb.PushLogsResponse, error) {
	if *collectedLogs == "" {
		return &pb.PushLogsResponse{
			StopSendingLogs: true,
		}, nil
	}

	peer, _, err := security.PeerFromContext(ctx)
	if err != nil {
		return nil, err
	}

	d.loggingMtx.Lock()
	defer d.loggingMtx.Unlock()

	if d.rotators[peer] == nil {
		filename := strings.Split(peer, "@")[0] + ".log"
		r, err := rotator.New(filepath.Join(*collectedLogs, filename), 10*1024, false, 10)
		if err != nil {
			return nil, err
		}
		d.rotators[peer] = r
	}

	r := d.rotators[peer]
	for _, m := range req.GetMessages() {
		if _, err := r.Write(m); err != nil {
			return nil, err
		}
	}
	return &pb.PushLogsResponse{}, nil
}
