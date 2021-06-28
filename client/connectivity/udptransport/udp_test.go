package udptransport_test

import (
	"context"
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

	connA, err := sockA.DialContext(ctx, sockB.LocalAddr().String())
	if err != nil {
		t.Fatalf("Failed to create connection from A: %v", err)
	}
	connB, err := sockB.DialContext(ctx, sockA.LocalAddr().String())
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
