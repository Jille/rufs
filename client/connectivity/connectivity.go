// Package connectivity connects to the discovery server and peers.
package connectivity

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

var (
	cmtx    sync.Mutex
	circles = map[string]*circle{}

	HandleResolveConflictRequest = func(ctx context.Context, req *pb.ResolveConflictRequest) error {
		return errors.New("connectivity.HandleResolveConflictRequest was not overridden")
	}
)

type circle struct {
	name        string
	client      pb.DiscoveryServiceClient
	myEndpoints []string
	keyPair     *security.KeyPair

	mtx   sync.Mutex
	peers map[string]*Peer
}

func ConnectToCircle(ctx context.Context, name string, port int, myEndpoints []string, myPort int, kp *security.KeyPair) error {
	conn, err := grpc.DialContext(ctx, net.JoinHostPort(name, fmt.Sprint(port)), grpc.WithTransportCredentials(credentials.NewTLS(kp.TLSConfigForMasterClient())), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("failed to connect to discovery server: %v", err)
	}
	client := pb.NewDiscoveryServiceClient(conn)

	if len(myEndpoints) == 0 {
		// Auto-detect endpoints
		res, err := client.GetMyIP(ctx, &pb.GetMyIPRequest{})
		if err != nil {
			return fmt.Errorf("no ips given and failed to retrieve IP from discovery server")
		}
		myEndpoints = []string{res.GetIp()}
	}

	// Add ports to endpoints that don't have any
	for i, e := range myEndpoints {
		_, _, err := net.SplitHostPort(e)
		if err != nil {
			myEndpoints[i] = net.JoinHostPort(e, fmt.Sprint(myPort))
		}
	}

	c := &circle{
		name:        name,
		client:      client,
		myEndpoints: myEndpoints,
		keyPair:     kp,
		peers:       map[string]*Peer{},
	}
	go c.run(ctx)
	circles[name] = c
	return nil
}

func (c *circle) run(ctx context.Context) {
	for {
		err := c.connect(ctx)
		if err != nil {
			log.Printf("Error talking to discovery server: %v", err)
			time.Sleep(time.Second)
		}
	}
}

func (c *circle) connect(ctx context.Context) error {
	stream, err := c.client.Connect(ctx, &pb.ConnectRequest{
		Endpoints: c.myEndpoints,
	}, grpc.WaitForReady(true))
	if err != nil {
		return fmt.Errorf("failed to subscribe to discovery server: %v", err)
	}
	log.Printf("Connected to RuFS. My endpoints: %s", c.myEndpoints)

	for {
		msg, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("discovery server stream error: %v", err)
		}
		if msg.GetPeerList() != nil {
			c.processPeers(ctx, msg.GetPeerList().GetPeers())
		}
	}
}

func (c *circle) processPeers(ctx context.Context, peers []*pb.Peer) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for _, pe := range peers {
		if po, existing := c.peers[pe.GetName()]; existing {
			po.update(pe)
		} else {
			c.peers[pe.GetName()] = c.newPeer(ctx, pe)
		}
	}
}

func (c *circle) newPeer(ctx context.Context, p *pb.Peer) *Peer {
	r := manual.NewBuilderWithScheme(fmt.Sprintf("rufs-%s", p.GetName()))
	r.InitialState(peerToResolverState(p))
	conn, err := grpc.DialContext(ctx, r.Scheme()+":///magic", grpc.WithResolvers(r), grpc.WithTransportCredentials(credentials.NewTLS(c.keyPair.TLSConfigForServerClient(p.GetName()))))
	if err != nil {
		log.Fatalf("Failed to dial peer %q: %v", r.Scheme(), err)
	}
	return &Peer{
		Name:     p.GetName(),
		conn:     conn,
		resolver: r,
	}
}

func peerToResolverState(p *pb.Peer) resolver.State {
	var s resolver.State
	for _, e := range p.GetEndpoints() {
		s.Addresses = append(s.Addresses, resolver.Address{
			Addr:       e,
			ServerName: p.GetName(),
		})
	}
	return s
}

type Peer struct {
	Name     string
	conn     *grpc.ClientConn
	resolver *manual.Resolver
}

func (p *Peer) update(pe *pb.Peer) {
	p.resolver.UpdateState(peerToResolverState(pe))
}

func (p *Peer) ContentServiceClient() pb.ContentServiceClient {
	return pb.NewContentServiceClient(p.conn)
}

func (c *circle) GetPeer(name string) *Peer {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.peers[name]
}

func (c *circle) AllPeers() []*Peer {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	all := make([]*Peer, 0, len(c.peers))
	for _, p := range c.peers {
		all = append(all, p)
	}
	return all
}

func GetPeer(name string) *Peer {
	cmtx.Lock()
	defer cmtx.Unlock()
	for _, c := range circles {
		if p := c.GetPeer(name); p != nil {
			return p
		}
	}
	return nil
}

func AllPeers() []*Peer {
	cmtx.Lock()
	defer cmtx.Unlock()
	var all []*Peer
	for _, c := range circles {
		all = append(all, c.AllPeers()...)
	}
	return all
}

func AllPeersInCircle(name string) []*Peer {
	return circles[name].AllPeers()
}

func DiscoveryClient(circle string) pb.DiscoveryServiceClient {
	return circles[circle].client
}
