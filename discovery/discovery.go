package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"

	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/grpc"
)

var (
	port = flag.Int("port", 0, "gRPC port")
)

func main() {
	flag.Parse()

	s := grpc.NewServer()
	pb.RegisterDiscoveryServiceService(s, pb.NewDiscoveryServiceService(&discovery{}))
	sock, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	if err := s.Serve(sock); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

type discovery struct{}

func (d *discovery) Connect(req *pb.ConnectRequest, stream pb.DiscoveryService_ConnectServer) error {
	return errors.New("not yet implemented")
}
