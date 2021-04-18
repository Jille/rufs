package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"time"

	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	circle        = flag.String("circle", "", "Name of the circle to join")
	discoveryPort = flag.Int("discovery-port", 12000, "Port of the discovery server")
	username      = flag.String("user", "", "RuFS username")
	token         = flag.String("token", "", "Auth token given by an administrator")
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	flag.Parse()

	if *circle == "" || *username == "" || *token == "" {
		log.Fatal("--circle, --username and --token are required")
	}

	tc, err := security.TLSConfigForRegistration("/tmp/rufs/ca.crt")
	if err != nil {
		log.Fatalf("Failed to load CA certificate: %v", err)
	}

	conn, err := grpc.DialContext(ctx, net.JoinHostPort(*circle, fmt.Sprint(*discoveryPort)), grpc.WithTransportCredentials(credentials.NewTLS(tc)), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Failed to connect to discovery server: %v", err)
	}
	defer conn.Close()
	c := pb.NewDiscoveryServiceClient(conn)

	privFile := fmt.Sprintf("/tmp/rufs/%s@%s.key", *username, *circle)
	pub, err := security.StoreNewKeyPair(privFile)
	if err != nil {
		log.Fatalf("Failed to generate key pair: %v", err)
	}

	resp, err := c.Register(ctx, &pb.RegisterRequest{
		Username:  *username,
		Token:     *token,
		PublicKey: pub,
	})
	if err != nil {
		os.Remove(privFile)
		log.Fatalf("Failed to register: %v", err)
	}

	if err := ioutil.WriteFile(fmt.Sprintf("/tmp/rufs/%s@%s.crt", *username, *circle), resp.GetCertificate(), 0644); err != nil {
		os.Remove(privFile)
		log.Fatalf("Failed to store certificate: %v", err)
	}
}
