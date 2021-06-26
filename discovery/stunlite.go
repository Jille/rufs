package main

import (
	"fmt"
	"log"
	"net"
)

func RunStun(port int) {
	laddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}
	sock, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		panic(err)
	}
	for {
		_, addr, err := sock.ReadFromUDP(nil)
		if err != nil {
			panic(err)
		}
		_, err = sock.WriteToUDP([]byte(addr.String()), addr)
		if err != nil {
			log.Printf("Failed to respond to stunlite packet: %v", err)
		}
	}
}
