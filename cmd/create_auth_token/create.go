package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/sgielen/rufs/security"
	"github.com/sgielen/rufs/version"
)

var (
	certdir = flag.String("certdir", "", "Where CA certs are read from (see create_ca_pair)")
)

func main() {
	flag.Parse()

	log.Printf("starting rufs create_auth_token %s", version.GetVersion())

	if *certdir == "" {
		log.Fatalf("Flag --certdir is required")
	}

	ca, err := security.LoadCAKeyPair(*certdir)
	if err != nil {
		log.Fatalf("Failed to load CA key pair: %v", err)
	}
	user := flag.Arg(0)
	if user == "" {
		log.Fatalf("Usage: %s <username>", os.Args[0])
	}
	token := ca.CreateToken(user)
	log.Printf("Auth token for %q is %q", user, token)
	fmt.Println(token)
}
