package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	discovery = flag.String("discovery", "127.0.0.1:12000", "RuFS Discovery server")
	username  = flag.String("user", "", "RuFS username")
	token     = flag.String("token", "", "Auth token given by an administrator")
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	flag.Parse()

	if *username == "" || *token == "" {
		log.Fatal("--username and --token are required")
	}

	tc, err := security.TLSConfigForRegistration("/tmp/rufs/ca.crt")
	if err != nil {
		log.Fatalf("Failed to load CA certificate: %v", err)
	}

	conn, err := grpc.Dial(*discovery, grpc.WithTransportCredentials(credentials.NewTLS(tc)), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Failed to connect to discovery server: %v", err)
	}
	defer conn.Close()
	c := pb.NewDiscoveryServiceClient(conn)

	privFile := fmt.Sprintf("/tmp/rufs/%s.key", *username)
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

	if err := ioutil.WriteFile(fmt.Sprintf("/tmp/rufs/%s.crt", *username), resp.GetCertificate(), 0644); err != nil {
		os.Remove(privFile)
		log.Fatalf("Failed to store certificate: %v", err)
	}
}
