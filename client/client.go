package main

import (
	"context"
	"flag"
	"io"
	"log"
	"strings"

	_ "github.com/Jille/grpc-multi-resolver"
	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/grpc"
)

var (
	discovery = flag.String("discovery", "127.0.0.1:12000", "RuFS Discovery server")
	username  = flag.String("user", "", "RuFS username")
	endpoint  = flag.String("endpoint", "127.0.0.1:12010", "Our RuFS endpoint")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	if *username == "" {
		log.Fatalf("username must not be empty (see -help)")
	}

	conn, err := grpc.Dial(*discovery, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("failed to connect to discovery server: %v", err)
	}
	defer conn.Close()
	c := pb.NewDiscoveryServiceClient(conn)

	stream, err := c.Connect(ctx, &pb.ConnectRequest{
		Username:  *username,
		Endpoints: strings.Split(*endpoint, ","),
	})
	if err != nil {
		log.Fatalf("failed to subscribe to discovery server: %v", err)
	}

	for {
		in, err := stream.Recv()
		if err == io.EOF {
			log.Fatalf("discovery server EOF, exiting")
		}
		if err != nil {
			log.Fatalf("discovery server stream error: %v", err)
		}
		readdir(ctx, in.Peers, "/")
	}
}

func readdir(ctx context.Context, peers []*pb.Peer, path string) {
	for _, peer := range peers {
		conn, err := grpc.Dial("multi:///"+strings.Join(peer.GetEndpoints(), ","), grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			log.Printf("failed to connect to peer %s, ignoring: %v", peer.GetUsername(), err)
			continue
		}
		defer conn.Close()

		c := pb.NewContentServiceClient(conn)
		r, err := c.ReadDir(ctx, &pb.ReadDirRequest{
			Path: path,
		})
		if err != nil {
			log.Printf("failed to readdir on peer %s, ignoring: %v", peer.GetUsername(), err)
			continue
		}
		log.Printf("readdir on peer %s: %s", peer.GetUsername(), r)
	}
}
