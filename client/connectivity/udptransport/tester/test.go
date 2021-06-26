package main

import (
	"flag"
	"log"
	"net"
	"time"

	"github.com/sgielen/rufs/client/connectivity/udptransport"
)

var (
	stunServer = flag.String("stun_server", "skynet.quis.cx:12000", "Address of Stunlite server")
)

func main() {
	flag.Parse()
	udpSocket, err := udptransport.New(handleIncomingContentConnection)
	if err != nil {
		log.Fatalf("Failed to enable gRPC-over-UDP: %v", err)
	}
	udpEndpoint, err := udpSocket.GetEndpointStunlite(*stunServer)
	if err != nil {
		log.Fatalf("Stunlite failed: %v", err)
	}
	log.Printf("Stunlite: %s", udpEndpoint)
	// udpSocket.DialContext(ctx, addr)
}

func handleIncomingContentConnection(s net.Conn) {
	go func() {
		for range time.Tick(time.Second) {
			if _, err := s.Write([]byte("Hail!")); err != nil {
				panic(err)
			}
		}
	}()
	go func() {
		for {
			var b [8192]byte
			n, err := s.Read(b[:])
			if err != nil {
				panic(err)
			}
			p := b[:n]
			log.Printf("Got packet from stream %d: %q", s.RemoteAddr(), string(p))
		}
	}()
}
