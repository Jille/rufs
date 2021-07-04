package udptransport_test

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"fmt"
	"io"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/sgielen/rufs/client/connectivity/udptransport"
)

func TestStuff(t *testing.T) {
	ctx := context.Background()

	sockA, err := udptransport.New(func(conn net.Conn) {
		handleEchoConnection(t, conn)
	})
	if err != nil {
		t.Fatalf("Failed to create listener A: %v", err)
	}
	sockB, err := udptransport.New(func(conn net.Conn) {
		handleEchoConnection(t, conn)
	})
	if err != nil {
		t.Fatalf("Failed to create listener B: %v", err)
	}

	sockAddrA := fmt.Sprintf("127.0.0.1:%d", sockA.LocalPort())
	sockAddrB := fmt.Sprintf("127.0.0.1:%d", sockB.LocalPort())

	connA, err := sockA.DialContext(ctx, sockAddrB)
	if err != nil {
		t.Fatalf("Failed to create connection from A: %v", err)
	}
	connB, err := sockB.DialContext(ctx, sockAddrA)
	if err != nil {
		t.Fatalf("Failed to create connection from B: %v", err)
	}

	// Write data over both sockets at the same time
	bufA := make([]byte, 1024*1024)
	if _, err := cryptoRand.Read(bufA); err != nil {
		t.Fatalf("Failed to generate random buffer A: %v", err)
	}
	bufB := make([]byte, 1024*1024)
	if _, err := cryptoRand.Read(bufB); err != nil {
		t.Fatalf("Failed to generate random buffer B: %v", err)
	}

	go sender(t, connA, bufA)
	go sender(t, connB, bufB)

	var recvBufA []byte
	readDone := make(chan struct{})
	go func() {
		recvBufA, err = io.ReadAll(connA)
		if err != nil {
			t.Fatalf("Failed to ReadAll on A: %v", err)
		}
		close(readDone)
	}()
	recvBufB, err := io.ReadAll(connB)
	if err != nil {
		t.Fatalf("Failed to ReadAll on B: %v", err)
	}
	<-readDone

	connA.Close()
	connB.Close()

	if !bytes.Equal(bufA, recvBufA) || !bytes.Equal(bufB, recvBufB) {
		t.Fatalf("Received buffers are not equal to sent ones")
	}
}

func handleEchoConnection(t *testing.T, c net.Conn) {
	_, err := io.Copy(c, c)
	if err != nil {
		t.Fatalf("Fatal error occurred in echo server: %v", err)
	}
}

func sender(t *testing.T, c net.Conn, buf []byte) {
	offset := 0
	for offset < len(buf) {
		n := len(buf) - offset
		if n > 5*1024 {
			n = 5 * 1024
		}
		n = rand.Intn(n + 1)
		n, err := c.Write(buf[offset : offset+n])
		if err != nil {
			t.Fatalf("write failed: %v", err)
		}
		offset += n

		ms := time.Duration(rand.Intn(10))
		time.Sleep(ms * time.Millisecond)
	}
	c.Close()
}
