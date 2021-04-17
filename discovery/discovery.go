package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"

	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/grpc"
)

var (
	port = flag.Int("port", 0, "gRPC port")
)

func main() {
	flag.Parse()

	d := &discovery{
		clients: map[string]*pb.Peer{},
	}
	d.cond = sync.NewCond(&d.mtx)
	s := grpc.NewServer()
	pb.RegisterDiscoveryServiceServer(s, d)
	sock, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	if err := s.Serve(sock); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

type discovery struct {
	pb.UnimplementedDiscoveryServiceServer

	mtx     sync.Mutex
	clients map[string]*pb.Peer
	streams map[string]pb.DiscoveryService_ConnectServer
	cond    *sync.Cond
}

func (d *discovery) Connect(req *pb.ConnectRequest, stream pb.DiscoveryService_ConnectServer) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	d.clients[req.GetUsername()] = &pb.Peer{
		Username:  req.GetUsername(),
		Endpoints: req.GetEndpoints(),
	}
	d.streams[req.GetUsername()] = stream
	d.cond.Broadcast()

	for {
		d.cond.Wait()
		if stream != d.streams[req.GetUsername()] {
			return errors.New("connection from another process for your username")
		}

		msg := &pb.ConnectResponse{}
		for _, p := range d.clients {
			msg.Peers = append(msg.Peers, p)
		}
		d.mtx.Unlock()
		if err := stream.Send(msg); err != nil {
			d.mtx.Lock()
			delete(d.clients, req.GetUsername())
			delete(d.streams, req.GetUsername())
			return err
		}
		d.mtx.Lock()
	}
}
