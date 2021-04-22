package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/golang/protobuf/proto"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

var (
	port = flag.Int("port", 12000, "gRPC port")
)

func main() {
	flag.Parse()

	ca, err := security.LoadCAKeyPair("/tmp/rufs/")
	if err != nil {
		log.Fatalf("Failed to load CA key pair: %v", err)
	}

	d := &discovery{
		ca:      ca,
		circle:  ca.Name(),
		clients: map[string]*client{},
	}
	d.cond = sync.NewCond(&d.mtx)
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(ca.TLSConfigForDiscovery())))
	pb.RegisterDiscoveryServiceServer(s, d)
	reflection.Register(s)
	sock, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	log.Printf("Listening on port %d for circle %q", *port, d.circle)
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
}

type client struct {
	peer                    *pb.Peer
	stream                  pb.DiscoveryService_ConnectServer
	newPeerList             bool
	newActiveDownloads      bool
	resolveConflictRequests []*pb.ResolveConflictRequest
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
			Name:      name,
			Endpoints: req.GetEndpoints(),
		},
		stream:             stream,
		newPeerList:        true,
		newActiveDownloads: true,
	}
	d.clients[name] = c
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
		for c.newPeerList || c.newActiveDownloads || len(c.resolveConflictRequests) > 0 {
			msg := &pb.ConnectResponse{}
			if len(c.resolveConflictRequests) > 0 {
				msg.Msg = &pb.ConnectResponse_ResolveConflictRequest{
					ResolveConflictRequest: c.resolveConflictRequests[0],
				}
				c.resolveConflictRequests = c.resolveConflictRequests[1:]
			} else if c.newActiveDownloads {
				msg.Msg = &pb.ConnectResponse_ActiveDownloads{
					ActiveDownloads: &pb.ConnectResponse_ActiveDownloadList{},
				}
				c.newActiveDownloads = false
			} else {
				msg.Msg = &pb.ConnectResponse_PeerList_{
					PeerList: &pb.ConnectResponse_PeerList{},
				}
				for _, c2 := range d.clients {
					msg.GetPeerList().Peers = append(msg.GetPeerList().Peers, c2.peer)
				}
				c.newPeerList = false
			}
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
