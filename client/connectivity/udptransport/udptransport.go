package udptransport

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/pion/logging"
	"github.com/pion/sctp"
)

type Socket struct {
	sock              *net.UDPConn
	newStreamCallback func(net.Conn)
	multiplexer       *udpMultiplexer
}

func New(newStreamCallback func(net.Conn)) (*Socket, error) {

	sock, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to enable gRPC-over-UDP: %v", err)
	}

	s := &Socket{
		sock:              sock,
		newStreamCallback: newStreamCallback,
	}
	logger := logging.NewDefaultLoggerFactory()
	s.multiplexer = newUDPMultiplexer(sock, func(c net.Conn) {
		assoc, err := sctp.Client(sctp.Config{
			NetConn:       c,
			LoggerFactory: logger,
		})
		_ = assoc
		_ = err
	})
	return s, nil
}

func (s *Socket) GetEndpointStunlite(addr string) (string, error) {
	log.Printf("gRPC-over-UDP transport local port = %d", s.sock.LocalAddr().(*net.UDPAddr).Port)
	raddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return "", err
	}

	res := [22]byte{}
	_, err = s.sock.WriteToUDP([]byte{}, raddr)
	if err != nil {
		return "", err
	}

	s.sock.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := s.sock.ReadFromUDP(res[:])
	if err != nil {
		return "", err
	}
	log.Printf("gRPC-over-UDP public endpoint = %s", res[:n])
	s.sock.SetReadDeadline(time.Time{})
	return string(res[:n]), nil
}

func (s *Socket) DialContext(ctx context.Context, addr string) (net.Conn, error) {
	return nil, fmt.Errorf("TODO: implement")
}
