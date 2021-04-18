package main

import (
	"flag"
	"log"

	"github.com/sgielen/rufs/security"
)

var (
	circle = flag.String("circle", "", "Hostname of the circle's discovery server")
)

func main() {
	flag.Parse()

	if *circle == "" {
		log.Fatal("Flag --circle is required")
	}

	if err := security.NewCA("/tmp/rufs", *circle); err != nil {
		log.Fatalf("Failed to create CA key pair: %v", err)
	}
}
