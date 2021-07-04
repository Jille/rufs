package udptransport_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/sgielen/rufs/client/connectivity/udptransport"
)

func TestStuff(t *testing.T) {
	ctx := context.Background()

	sockA, err := udptransport.New(handleConnectionForA)
	if err != nil {
		t.Fatalf("Failed to create listener A: %v", err)
	}
	sockB, err := udptransport.New(handleConnectionForB)
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

	connA.Close()
	connB.Close()
}

func handleConnectionForA(c net.Conn) {
}

func handleConnectionForB(c net.Conn) {
}
