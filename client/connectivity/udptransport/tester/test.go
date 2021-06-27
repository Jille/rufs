package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strings"

	"github.com/sgielen/rufs/client/connectivity/udptransport"
)

var (
	stunServer = flag.String("stun_server", "skynet.quis.cx:12000", "Address of Stunlite server")
	send       = flag.Bool("send", true, "Whether this instance will send or only receive")
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
	select {}
}

func handleConnection(s net.Conn) {
	if *send {
		go func() {
			fh, err := os.Open("/tmp/vmlinuz-5.11.0-22-generic")
			if err != nil {
				panic(err)
			}
			defer fh.Close()
			if _, err := io.Copy(s, fh); err != nil {
				panic(err)
			}
			if err := s.Close(); err != nil {
				panic(err)
			}
		}()
	}

	go func() {
		for {
			var b [8192]byte
			n, err := s.Read(b[:])
			if err != nil {
				if err == io.EOF {
					log.Printf("EOF on reader")
					return
				}
				panic(err)
			}
			p := b[:n]
			log.Printf("Got packet from stream %s: %q", s.RemoteAddr(), string(p))
		}
	}()
}
