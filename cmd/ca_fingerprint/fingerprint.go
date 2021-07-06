package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/sgielen/rufs/security"
	"github.com/sgielen/rufs/version"
)

var (
	certdir = flag.String("certdir", "", "Where CA certs are read from (see create_ca_pair)")
)

func main() {
	flag.Parse()

	log.Printf("starting rufs ca_fingerprint %s", version.GetVersion())

	if *certdir == "" {
		log.Fatalf("Flag --certdir is required")
	}

	ca, err := security.LoadCAKeyPair(*certdir)
	if err != nil {
		log.Fatalf("Failed to load CA key pair: %v", err)
	}
	fp := ca.Fingerprint()
	log.Printf("CA fingerprint for %q is %q", ca.Name(), fp)
	fmt.Println(fp)
}
