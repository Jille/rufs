package udptransport

import (
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Jille/rufs/client/connectivity/udptransport/deadlinech"
)

const mtu = 1400

var pool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, mtu+1)
		return &buf
	},
}

type udpMultiplexer struct {
	sock            *net.UDPConn
	newPeerCallback func(net.Conn)
	hadWrites       int32

	mtx         sync.Mutex
	connections map[string]*semiConnectedUDP
}

func newUDPMultiplexer(sock *net.UDPConn, newPeerCallback func(net.Conn)) *udpMultiplexer {
	m := &udpMultiplexer{
		sock:            sock,
		newPeerCallback: newPeerCallback,
		connections:     map[string]*semiConnectedUDP{},
	}
	go m.reader()
	return m
}

// GetBlocking returns the connection to a peer, possibly calling newPeerCallback if it didn't exist yet.
// GetBlocking guarantees that newPeerCallback has returned before it returns itself.
func (m *udpMultiplexer) GetBlocking(addr *net.UDPAddr) net.Conn {
	a := m.get(addr, m.newPeerCallback)
	<-a.initialized
	return a
}

func (m *udpMultiplexer) GetBypassCallback(addr *net.UDPAddr) net.Conn {
	return m.get(addr, func(net.Conn) {})
}

func (m *udpMultiplexer) GetHadWritesAndClear() bool {
	return atomic.SwapInt32(&m.hadWrites, 0) != 0
}

func (m *udpMultiplexer) get(addr *net.UDPAddr, newPeerCallback func(net.Conn)) *semiConnectedUDP {
	key := addr.String()
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if c, ok := m.connections[key]; ok {
		return c
	}
	c := &semiConnectedUDP{
		udpConn:      m.sock,
		remoteAddr:   addr,
		msgs:         make(chan message, 100),
		quit:         make(chan struct{}),
		initialized:  make(chan struct{}),
		readDeadline: deadlinech.New(),
		onWrite:      func() { atomic.StoreInt32(&m.hadWrites, 1) },
	}
	m.connections[key] = c
	go func() {
		newPeerCallback(c)
		close(c.initialized)
	}()
	return c
}

func (m *udpMultiplexer) reader() {
	for {
		buf := pool.Get().(*[]byte)
		n, addr, err := m.sock.ReadFromUDP((*buf)[:])
		if err != nil {
			panic(err)
		}
		if n > mtu {
			// Message is too large. Shouldn't happen with our SCTP settings. Dropping it.
			continue
		}
		msg := message{
			data:  (*buf)[:n],
			peer:  addr,
			alloc: buf,
		}
		c := m.get(addr, m.newPeerCallback)
		select {
		case c.msgs <- msg:
		default:
			// Channel is full. Drop the packet.
		}
	}
}

type message struct {
	peer  *net.UDPAddr
	data  []byte
	alloc *[]byte
}

type semiConnectedUDP struct {
	udpConn    *net.UDPConn
	remoteAddr *net.UDPAddr

	msgs         chan message
	quit         chan struct{}
	initialized  chan struct{}
	readDeadline *deadlinech.DeadlineChannel
	onWrite      func()
}

func (t *semiConnectedUDP) Write(p []byte) (int, error) {
	select {
	case <-t.quit:
		return 0, errors.New("Write on a closed semiConnectedUDP")
	default:
		t.onWrite()
		return t.udpConn.WriteToUDP(p, t.remoteAddr)
	}
}

func (t *semiConnectedUDP) Read(p []byte) (int, error) {
	for {
		select {
		case m := <-t.msgs:
			n := copy(p, m.data)
			pool.Put(m.alloc)
			return n, nil
		case <-t.quit:
			return 0, errors.New("semiConnectedUDP connection was closed")
		case <-t.readDeadline.Wait():
			return 0, deadlineExceeded{}
		}
	}
}

func (t *semiConnectedUDP) Close() error {
	close(t.quit)
	return nil
}

func (t *semiConnectedUDP) LocalAddr() net.Addr {
	return t.udpConn.LocalAddr()
}

func (t *semiConnectedUDP) RemoteAddr() net.Addr {
	return t.remoteAddr
}

func (t *semiConnectedUDP) SetDeadline(ts time.Time) error {
	if err := t.SetReadDeadline(ts); err != nil {
		return err
	}
	if err := t.SetWriteDeadline(ts); err != nil {
		return err
	}
	return nil
}

func (t *semiConnectedUDP) SetReadDeadline(ts time.Time) error {
	t.readDeadline.Set(ts)
	return nil
}

func (t *semiConnectedUDP) SetWriteDeadline(ts time.Time) error {
	log.Printf("Ignoring SetWriteDeadline(%s) call", ts)
	return nil
}
