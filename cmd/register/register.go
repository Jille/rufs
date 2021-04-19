package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sgielen/rufs/config"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	circle        = flag.String("circle", "", "Name of the circle to join")
	caFlag        = flag.String("ca", "/tmp/rufs/ca.crt", "Path or URL to the CA certificate")
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
	config.MustResolvePath()

	ca, err := readOrFetch(*caFlag)
	if err != nil {
		log.Fatalf("Failed to read CA certificate from %q: %v", *caFlag, err)
	}

	tc, err := security.TLSConfigForRegistration(ca)
	if err != nil {
		log.Fatalf("Failed to load CA certificate: %v", err)
	}

	conn, err := grpc.DialContext(ctx, net.JoinHostPort(*circle, fmt.Sprint(*discoveryPort)), grpc.WithTransportCredentials(credentials.NewTLS(tc)), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Failed to connect to discovery server: %v", err)
	}
	defer conn.Close()
	c := pb.NewDiscoveryServiceClient(conn)

	key, err := security.NewKey()
	if err != nil {
		log.Fatalf("Failed to generate key pair: %v", err)
	}
	pub, err := key.SerializePublicKey()
	if err != nil {
		log.Fatalf("Failed to serialize public key: %v", err)
	}

	resp, err := c.Register(ctx, &pb.RegisterRequest{
		Username:  *username,
		Token:     *token,
		PublicKey: pub,
	})
	if err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	caFile, crtFile, keyFile := config.PKIFiles(*circle)
	if err := key.StorePrivateKey(keyFile); err != nil {
		log.Fatalf("Failed to store private key: %v", err)
	}

	if err := ioutil.WriteFile(crtFile, resp.GetCertificate(), 0644); err != nil {
		os.Remove(keyFile)
		log.Fatalf("Failed to store certificate: %v", err)
	}

	if err := ioutil.WriteFile(caFile, ca, 0644); err != nil {
		os.Remove(keyFile)
		os.Remove(crtFile)
		log.Fatalf("Failed to store CA certificate: %v", err)
	}
}

func readOrFetch(uri string) ([]byte, error) {
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		if strings.HasPrefix(uri, "http://") {
			log.Print("Warning: Retrieving certificates over unencrypted http is unsafe")
		}

		resp, err := http.Get(uri)
		if err != nil {
			return nil, fmt.Errorf("HTTP request failed: %v", err)
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP requested failed with %q", resp.Status)
		}
		defer resp.Body.Close()
		return ioutil.ReadAll(resp.Body)
	}
	return ioutil.ReadFile(uri)
}
