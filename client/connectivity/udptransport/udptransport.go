package udptransport

import (
	"context"
	"net"
	"sync"
)

type Socket struct {
	sock              *net.UDPConn
	newStreamCallback func(*sctp.Stream)
	multiplexer *udpMultiplexer
}

func New(sock *net.UDPConn, newStreamCallback func(*sctp.Stream)) (*Socket, error) {
	s := &Socket{
		sock:              sock,
		newStreamCallback: newStreamCallback,
		associations:      map[string]*sctp.Association{},
	}
	logger := logging.NewDefaultLoggerFactory()
	s.multiplexer = newUDPMultiplexer(sock, func(c net.Conn) {
		assoc, err := sctp.Client(sctp.Config{
			NetConn: c,
			LoggerFactory: logger,
		})
	})
	return s, nil
}

func (s *Socket) EstablishAssociation(ctx context.Context, addr string, newStreamCallback func(*sctp.Stream)) (*sctp.Association, error) {
}

func (s *Socket) Dial(ctx context.Context, addr string) (net.Conn, error) {
}
