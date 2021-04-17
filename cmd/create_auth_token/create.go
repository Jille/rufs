package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/sgielen/rufs/security"
)

func main() {
	flag.Parse()

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
