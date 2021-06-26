package udptransport

import (
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

var pool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 8192)
	},
}

type udpMultiplexer struct {
	sock            *net.UDPConn
	newPeerCallback func(net.Conn)

	mtx         sync.Mutex
	connections map[string]*semiConnectedUDP
}

func newUDPMultiplexer(sock *net.UDPConn, newPeerCallback func(net.Conn)) *udpMultiplexer {
	m := &udpMultiplexer{
		sock:            sock,
		newPeerCallback: newPeerCallback,
	}
	go m.reader()
	return m
}

func (m *udpMultiplexer) reader() {
	for {
		buf := pool.Get().([]byte)
		n, addr, err := s.sock.ReadFromUDP(buf[:])
		if err != nil {
			panic(err)
		}
		m := message{
			data: buf[:n],
			peer: addr,
		}
		key := addr.String()
		s.mtx.Lock()
		a, ok := s.connections[key]
		if !ok {
			a = &semiConnectedUDP{
				udpConn:    s.sock,
				remoteAddr: addr,
				msgs:       make(chan message, 100),
			}
			s.connections[key] = a
			go m.newPeerCallback(a)
		}
		s.mtx.Unlock()
		select {
		case a.msgs <- m:
		default:
			// Channel is full. Drop the packet.
		}
	}
}

type message struct {
	peer *net.UDPAddr
	data []byte
}

type semiConnectedUDP struct {
	udpConn    *net.UDPConn
	remoteAddr *net.UDPAddr

	msgs chan message
	quit chan struct{}
}

func (t *semiConnectedUDP) Write(p []byte) (int, error) {
	select {
	case <-t.quit:
		return 0, errors.New("Write on a closed semiConnectedUDP")
	default:
		return t.udpConn.WriteToUDP(p, t.remoteAddr)
	}
}

func (t *semiConnectedUDP) Read(p []byte) (int, error) {
	select {
	case m := <-t.msgs:
		n := copy(p, m.data)
		pool.Put(m.data)
		return n, nil
	case <-t.quit:
		return errors.New("emiConnectedUDP connection was closed")
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
	log.Printf("Ignoring SetDeadline(%s) call", ts)
	return nil
}

func (t *semiConnectedUDP) SetReadDeadline(ts time.Time) error {
	log.Printf("Ignoring SetReadDeadline(%s) call", ts)
	return nil
}

func (t *semiConnectedUDP) SetWriteDeadline(ts time.Time) error {
	log.Printf("Ignoring SetWriteDeadline(%s) call", ts)
	return nil
}
