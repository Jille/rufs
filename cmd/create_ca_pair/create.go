package main

import (
	"flag"
	"log"
	"os"

	"github.com/sgielen/rufs/security"
	"github.com/sgielen/rufs/version"
)

var (
	circle = flag.String("circle", "", "Hostname of the circle's discovery server")
)

func main() {
	flag.Parse()

	log.Printf("starting rufs %s", version.GetVersion())

	if *circle == "" {
		log.Fatal("Flag --circle is required")
	}

	if err := os.MkdirAll("/tmp/rufs", 0755); err != nil {
		log.Fatalf("Failed to create /tmp/rufs: %v", err)
	}

	if err := security.NewCA("/tmp/rufs", *circle); err != nil {
		log.Fatalf("Failed to create CA key pair: %v", err)
	}
}
