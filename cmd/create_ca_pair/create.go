package main

import (
	"flag"
	"log"

	"github.com/sgielen/rufs/security"
)

func main() {
	flag.Parse()

	if err := security.NewCA("/tmp/rufs", "rufs-ca"); err != nil {
		log.Fatalf("Failed to create CA key pair: %v", err)
	}
}
