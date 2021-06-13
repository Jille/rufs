package main

import (
	"context"
	"flag"
	"log"

	"github.com/sgielen/rufs/client/config"
	"github.com/sgielen/rufs/client/register"
	"github.com/sgielen/rufs/version"
)

var (
	circle   = flag.String("circle", "", "Name of the circle to join")
	ca       = flag.String("ca", "", "Path or URL to the CA certificate")
	username = flag.String("user", "", "RuFS username")
	token    = flag.String("token", "", "Auth token given by an administrator")
)

func main() {
	flag.Parse()

	log.Printf("starting rufs %s", version.GetVersion())

	if *circle == "" || *ca == "" || *username == "" || *token == "" {
		log.Fatal("--circle, --ca, --username and --token are required")
	}
	config.MustResolvePath()

	if err := register.Register(context.Background(), *circle, *username, *token, *ca); err != nil {
		log.Fatalf("Registration failed: %v", err)
	}
}
