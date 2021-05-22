package security

import (
	"context"
	"net"
	"runtime"
	"sync"

	"google.golang.org/grpc/credentials"
)

// SwappableCredentials wraps credentials.TransportCredentials that can be replaced at runtime.
type SwappableCredentials struct {
	mtx        sync.Mutex
	backend    credentials.TransportCredentials
	serverName *string
	children   []*SwappableCredentials
}

func NewSwappableCredentials(backend credentials.TransportCredentials) *SwappableCredentials {
	return &SwappableCredentials{
		backend: backend,
	}
}

func (s *SwappableCredentials) Info() credentials.ProtocolInfo {
	s.mtx.Lock()
	be := s.backend
	s.mtx.Unlock()
	return be.Info()
}

func (s *SwappableCredentials) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (_ net.Conn, _ credentials.AuthInfo, err error) {
	s.mtx.Lock()
	be := s.backend
	s.mtx.Unlock()
	return be.ClientHandshake(ctx, authority, rawConn)
}

func (s *SwappableCredentials) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	s.mtx.Lock()
	be := s.backend
	s.mtx.Unlock()
	return be.ServerHandshake(rawConn)
}

func (s *SwappableCredentials) Clone() credentials.TransportCredentials {
	s.mtx.Lock()
	n := NewSwappableCredentials(s.backend.Clone())
	s.children = append(s.children, n)
	s.mtx.Unlock()
	w := &clonedCredentials{n}
	runtime.SetFinalizer(w, s.garbageCollectClone)
	return n
}

type clonedCredentials struct {
	*SwappableCredentials
}

func (s *SwappableCredentials) garbageCollectClone(w *clonedCredentials) {
	s.mtx.Lock()
	for i, c := range s.children {
		if c == w.SwappableCredentials {
			s.children[i] = s.children[len(s.children)-1]
			s.children = s.children[:len(s.children)-2]
			break
		}
	}
	s.mtx.Unlock()
}

func (s *SwappableCredentials) OverrideServerName(serverNameOverride string) error {
	s.mtx.Lock()
	be := s.backend
	s.serverName = &serverNameOverride
	s.mtx.Unlock()
	return be.OverrideServerName(serverNameOverride)
}

func (s *SwappableCredentials) Swap(backend credentials.TransportCredentials) {
	s.mtx.Lock()
	for _, c := range s.children {
		c.Swap(backend.Clone())
	}
	if s.serverName != nil {
		backend.OverrideServerName(*s.serverName)
	}
	s.backend = backend
	s.mtx.Unlock()
}
