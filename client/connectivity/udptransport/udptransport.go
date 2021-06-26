package udptransport

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/sctp"
)

type Socket struct {
	sock              *net.UDPConn
	newStreamCallback func(net.Conn)
	multiplexer       *udpMultiplexer
	loggerFactory     logging.LoggerFactory

	mtx          sync.Mutex
	associations map[string]*association
}

type association struct {
	assoc        *sctp.Association
	nextStreamId uint16
}

func New(newStreamCallback func(net.Conn)) (*Socket, error) {
	sock, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to enable gRPC-over-UDP: %v", err)
	}

	s := &Socket{
		sock:              sock,
		newStreamCallback: newStreamCallback,
		loggerFactory:     logging.NewDefaultLoggerFactory(),
		associations:      map[string]*association{},
	}
	s.multiplexer = newUDPMultiplexer(sock, s.handleNewConnection)
	return s, nil
}

func (s *Socket) GetEndpointStunlite(addr string) (string, error) {
	log.Printf("gRPC-over-UDP transport local port = %d", s.sock.LocalAddr().(*net.UDPAddr).Port)
	raddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return "", err
	}

	sock := s.multiplexer.GetBypassCallback(raddr)

	_, err = sock.Write(nil)
	if err != nil {
		return "", err
	}

	sock.SetReadDeadline(time.Now().Add(5 * time.Second))
	res := [22]byte{}
	n, err := sock.Read(res[:])
	if err != nil {
		return "", err
	}
	log.Printf("gRPC-over-UDP public endpoint = %s", res[:n])
	sock.SetReadDeadline(time.Time{})
	return string(res[:n]), nil
}

func (s *Socket) handleNewConnection(c net.Conn) {
	assoc, err := sctp.Client(sctp.Config{
		NetConn:        c,
		MaxMessageSize: 1400,
		LoggerFactory:  s.loggerFactory,
	})
	if err != nil {
		log.Printf("Failed to create association with %s: %v", c.RemoteAddr().String(), err)
		c.Close()
		return
	}
	stream1Ch := make(chan *sctp.Stream, 1)
	go s.handleIncomingStreams(assoc, c.RemoteAddr(), stream1Ch)
	var odd bool
	if stream, err := assoc.OpenStream(1, sctp.PayloadTypeWebRTCBinary); err == nil {
		odd, err = s.negotiate(stream)
	} else {
		odd, err = s.negotiate(<-stream1Ch)
	}
	if err != nil {
		log.Printf("Failed to negotiate with %s: %v", c.RemoteAddr().String(), err)
		c.Close()
		return
	}
	nextStreamId := uint16(2)
	if odd {
		nextStreamId = 3
	}
	s.mtx.Lock()
	s.associations[c.RemoteAddr().String()] = &association{
		assoc:        assoc,
		nextStreamId: nextStreamId,
	}
	s.mtx.Unlock()
}

func (s *Socket) handleIncomingStreams(assoc *sctp.Association, raddr net.Addr, stream1Ch chan *sctp.Stream) {
	for {
		stream, err := assoc.AcceptStream()
		if err != nil {
			// AcceptStream only returns io.EOF.
			return
		}
		if stream.StreamIdentifier() == 1 {
			select {
			case stream1Ch <- stream:
			default:
			}
			continue
		}
		s.newStreamCallback(&sctpStreamWrapper{
			stream:     stream,
			remoteAddr: raddr,
		})
	}
}

func (s *Socket) negotiate(stream1 *sctp.Stream) (bool, error) {
	defer stream1.Close()
	myRand := make([]byte, 8)
	if _, err := rand.Read(myRand); err != nil {
		return false, err
	}
	if _, err := stream1.Write(myRand); err != nil {
		return false, err
	}
	theirRand := make([]byte, 8)
	if _, err := io.ReadFull(stream1, theirRand); err != nil {
		return false, err
	}
	mine := binary.LittleEndian.Uint64(myRand)
	theirs := binary.LittleEndian.Uint64(theirRand)
	return mine < theirs, nil
}

func (s *Socket) DialContext(ctx context.Context, addr string) (net.Conn, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	// GetBlocking blocks until handleNewConnection() returns.
	s.multiplexer.GetBlocking(raddr)
	s.mtx.Lock()
	a, ok := s.associations[raddr.String()]
	if ok {
		a.nextStreamId += 2
	}
	s.mtx.Unlock()
	if !ok {
		return nil, errors.New("strange race condition where the association isn't known. The again later")
	}
	stream, err := a.assoc.OpenStream(a.nextStreamId, sctp.PayloadTypeWebRTCBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to OpenStream on SCTP association: %v", err)
	}
	return &sctpStreamWrapper{
		stream:     stream,
		remoteAddr: raddr,
	}, nil
}

type sctpStreamWrapper struct {
	stream     *sctp.Stream
	remoteAddr net.Addr
}

func (w *sctpStreamWrapper) Write(p []byte) (int, error) {
	return w.stream.Write(p)
}

func (w *sctpStreamWrapper) Read(p []byte) (int, error) {
	return w.stream.Read(p)
}

func (w *sctpStreamWrapper) Close() error {
	return w.stream.Close()
}

func (w *sctpStreamWrapper) LocalAddr() net.Addr {
	return sctpAddr{}
}

type sctpAddr struct{}

func (sctpAddr) Network() string { return "sctp" }
func (sctpAddr) String() string  { return "SCTP-over-UDP" }

func (w *sctpStreamWrapper) RemoteAddr() net.Addr {
	return w.remoteAddr
}

func (w *sctpStreamWrapper) SetDeadline(ts time.Time) error {
	log.Printf("Ignoring SetDeadline(%s) call", ts)
	return nil
}

func (w *sctpStreamWrapper) SetReadDeadline(ts time.Time) error {
	log.Printf("Ignoring SetReadDeadline(%s) call", ts)
	return nil
}

func (w *sctpStreamWrapper) SetWriteDeadline(ts time.Time) error {
	log.Printf("Ignoring SetWriteDeadline(%s) call", ts)
	return nil
}
