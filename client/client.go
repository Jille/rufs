package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	_ "github.com/Jille/grpc-multi-resolver"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

	tc, err := security.TLSConfigForMasterClient("/tmp/rufs/ca.crt", fmt.Sprintf("/tmp/rufs/%s.crt", *username), fmt.Sprintf("/tmp/rufs/%s.key", *username))
	if err != nil {
		log.Fatalf("Failed to load certificates: %v", err)
	}

	conn, err := grpc.DialContext(ctx, *discovery, grpc.WithTransportCredentials(credentials.NewTLS(tc)), grpc.WithBlock())
	if err != nil {
		log.Fatalf("failed to connect to discovery server: %v", err)
	}
	defer conn.Close()
	c := pb.NewDiscoveryServiceClient(conn)

	stream, err := c.Connect(ctx, &pb.ConnectRequest{
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	type peerFile struct {
		peers []*pb.Peer
	}

	warnings := []string{}
	files := make(map[string]*peerFile)

	for _, peer := range peers {
		conn, err := grpc.DialContext(ctx, "multi:///"+strings.Join(peer.GetEndpoints(), ","), grpc.WithInsecure(), grpc.WithBlock())
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
		for _, file := range r.Files {
			if files[file.Filename] == nil {
				files[file.Filename] = &peerFile{}
			}
			files[file.Filename].peers = append(files[file.Filename].peers, peer)
		}
	}

	// remove files available on multiple peers
	for filename, file := range files {
		if len(file.peers) != 1 {
			peers := ""
			for _, peer := range file.peers {
				if peers == "" {
					peers = peer.GetUsername()
				} else {
					peers = peers + ", " + peer.GetUsername()
				}
			}
			warnings = append(warnings, fmt.Sprintf("File %s is available on multiple peers (%s), so it was hidden.", filename, peers))
			delete(files, filename)
		}
	}

	log.Printf("files in %s:", path)
	for filename, file := range files {
		log.Printf("- %s (%s)", filename, *file)
	}
	if len(warnings) >= 1 {
		log.Printf("warnings:")
		for _, warning := range warnings {
			log.Printf("- %s", warning)
		}
	}
}
