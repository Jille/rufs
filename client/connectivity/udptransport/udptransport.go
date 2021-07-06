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

	"github.com/cenkalti/backoff/v4"
	"github.com/pion/logging"
	"github.com/pion/sctp"
	"github.com/sgielen/rufs/client/connectivity/udptransport/deadlinech"
)

const writeBufferSize = 16384

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

func (s *Socket) GetEndpointStunlite(ctx context.Context, addr string) (string, error) {
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

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() {
		b := backoff.NewExponentialBackOff()
		b.InitialInterval = 100 * time.Millisecond
		_ = backoff.Retry(func() error {
			_, err := sock.Write(nil)
			return err
		}, backoff.WithContext(b, ctx))
	}()

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

func (s *Socket) LocalAddr() net.Addr {
	return s.sock.LocalAddr()
}

func (s *Socket) LocalPort() int {
	return s.sock.LocalAddr().(*net.UDPAddr).Port
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
	stream, err := assoc.OpenStream(1, sctp.PayloadTypeWebRTCBinary)
	if err == nil {
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
		log.Printf("AcceptStream returned %d from %s", stream.StreamIdentifier(), raddr.String())
		s.newStreamCallback(wrapSctpStream(stream, raddr))
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
		if a.nextStreamId <= 1 {
			// Skip stream 0 (unusable) and 1 (negotiation channel).
			a.nextStreamId += 2
		}
	}
	s.mtx.Unlock()
	if !ok {
		return nil, errors.New("failed to establish SCTP association")
	}
	stream, err := a.assoc.OpenStream(a.nextStreamId, sctp.PayloadTypeWebRTCBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to OpenStream on SCTP association: %v", err)
	}
	log.Printf("OpenStream(%d) when dialing to %q", stream.StreamIdentifier(), addr)
	return wrapSctpStream(stream, raddr), nil
}

func wrapSctpStream(stream *sctp.Stream, raddr net.Addr) net.Conn {
	stream.SetBufferedAmountLowThreshold(writeBufferSize)
	ret := &sctpStreamWrapper{
		stream:        stream,
		remoteAddr:    raddr,
		readerResult:  make(chan error, 1),
		readDeadline:  deadlinech.New(),
		pushbackCh:    make(chan struct{}, 1),
		writeDeadline: deadlinech.New(),
	}
	ret.readerResult <- nil
	stream.OnBufferedAmountLow(ret.bufferedAmountLow)
	return ret
}

type sctpStreamWrapper struct {
	stream     *sctp.Stream
	remoteAddr net.Addr

	readMtx      sync.Mutex
	readerResult chan error
	readBuf      [2048]byte // must be at least mtu-28
	readable     []byte
	readDeadline *deadlinech.DeadlineChannel

	writeMtx      sync.Mutex
	pushbackMtx   sync.Mutex
	pushbackCh    chan struct{}
	writeDeadline *deadlinech.DeadlineChannel
}

func (w *sctpStreamWrapper) Write(p []byte) (int, error) {
	w.writeMtx.Lock()
	defer w.writeMtx.Unlock()
	sent := 0
	for len(p) > 0 {
		f := p
		if len(f) > mtu-28 {
			f = p[:mtu-28]
		}
		p = p[len(f):]
		if err := w.waitForBufferSpace(); err != nil {
			return sent, err
		}
		n, err := w.stream.Write(f)
		sent += n
		if err != nil {
			return sent, err
		}
	}
	return sent, nil
}

func (w *sctpStreamWrapper) waitForBufferSpace() error {
	// Callers must hold w.writeMtx.
	w.pushbackMtx.Lock()
	for w.stream.BufferedAmount() > writeBufferSize {
		w.pushbackMtx.Unlock()
		select {
		case <-w.pushbackCh:
		case <-w.writeDeadline.Wait():
			return deadlineExceeded{}
		}
		w.pushbackMtx.Lock()
	}
	w.pushbackMtx.Unlock()
	return nil
}

func (w *sctpStreamWrapper) bufferedAmountLow() {
	w.pushbackMtx.Lock()
	defer w.pushbackMtx.Unlock()
	select {
	case w.pushbackCh <- struct{}{}:
	default:
	}
}

func (w *sctpStreamWrapper) Read(p []byte) (int, error) {
	// We need to read the entire SCTP packet in a single read. But our callers might pass a buffer that's smaller than one packet. We also need to handle the read readline.
	// w.readerResult is used as a mutex. If you can read from it, you own it. You need to own it to use w.readable and w.readBuf. After returning, there should either be a value in the channel or there should be a goroutine running that will eventually write a value.
	w.readMtx.Lock()
	defer w.readMtx.Unlock()
	for {
		var err error
		select {
		case <-w.readDeadline.Wait():
			return 0, deadlineExceeded{}
		case err = <-w.readerResult: // This should only block if there is a goroutine reading from w.stream active.
		}
		// We've read from w.readerResult, so we hold that "lock".
		if len(w.readable) > 0 {
			n := copy(p, w.readable)
			w.readable = w.readable[n:]
			if err != nil && len(w.readable) > 0 {
				// Defer the error to the next read.
				w.readerResult <- err
				return n, nil
			}
			w.readerResult <- nil
			return n, err
		}
		if err != nil {
			w.readerResult <- nil
			return 0, err
		}
		go func() {
			// Note that guarantee that we pass in a large enough buffer to read the entire SCTP packet here.
			n, err := w.stream.Read(w.readBuf[:])
			w.readable = w.readBuf[:n]
			w.readerResult <- err
		}()
	}
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
	w.SetWriteDeadline(ts)
	w.SetReadDeadline(ts)
	return nil
}

func (w *sctpStreamWrapper) SetReadDeadline(ts time.Time) error {
	w.readDeadline.Set(ts)
	return nil
}

func (w *sctpStreamWrapper) SetWriteDeadline(ts time.Time) error {
	w.writeDeadline.Set(ts)
	return nil
}
