package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/sgielen/rufs/security"
	"github.com/sgielen/rufs/version"
)

func main() {
	flag.Parse()

	log.Printf("starting rufs %s", version.GetVersion())

	ca, err := security.LoadCAKeyPair("/tmp/rufs/")
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
