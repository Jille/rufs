package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/sgielen/rufs/client/connectivity/udptransport"
)

var (
	stunServer = flag.String("stun_server", "skynet.quis.cx:12000", "Address of Stunlite server")
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile | log.Lmicroseconds)
	ctx := context.Background()
	flag.Parse()
	udpSocket, err := udptransport.New(handleConnection)
	if err != nil {
		log.Fatalf("Failed to enable gRPC-over-UDP: %v", err)
	}
	udpEndpoint, err := udpSocket.GetEndpointStunlite(*stunServer)
	if err != nil {
		log.Fatalf("Stunlite failed: %v", err)
	}
	log.Printf("Stunlite: %s", udpEndpoint)

	var remoteB [128]byte
	n, err := os.Stdin.Read(remoteB[:])
	if err != nil {
		panic(err)
	}
	remote := strings.TrimSpace(string(remoteB[:n]))
	log.Printf("Okay. %q is my peer", remote)

	conn, err := udpSocket.DialContext(ctx, remote)
	if err != nil {
		panic(err)
	}
	handleConnection(conn)
	select{}
}

func handleConnection(s net.Conn) {
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
			log.Printf("Got packet from stream %s: %q", s.RemoteAddr(), string(p))
		}
	}()
}
