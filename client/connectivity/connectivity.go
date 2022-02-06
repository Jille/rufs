// Package connectivity connects to the discovery server and peers.
package connectivity

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Jille/rpcz"
	"github.com/sgielen/rufs/client/connectivity/udptransport"
	"github.com/sgielen/rufs/client/remotelogging"
	"github.com/sgielen/rufs/common"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"github.com/sgielen/rufs/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

var (
	cmtx    sync.Mutex
	circles = map[string]*circle{}

	HandleResolveConflictRequest    = func(ctx context.Context, req *pb.ResolveConflictRequest, circle string) {}
	HandleActiveDownloadList        = func(ctx context.Context, req *pb.ConnectResponse_ActiveDownloadList, circle string) {}
	HandleIncomingContentConnection = func(net.Conn) {}

	nextManualResolverScheme int
)

type circle struct {
	name        string
	client      pb.DiscoveryServiceClient
	myPort      int
	udpSocket   *udptransport.Socket
	myEndpoints []string
	keyPair     *security.KeyPair

	mtx   sync.Mutex
	peers map[string]*Peer
}

func ConnectToCircle(ctx context.Context, name string, myEndpoints []string, myPort int, kp *security.KeyPair) error {
	cmtx.Lock()
	_, found := circles[name]
	cmtx.Unlock()
	if found {
		return nil
	}

	addr, port, err := net.SplitHostPort(name)
	if err != nil {
		addr = name
		port = "12000"
	}

	conn, err := grpc.DialContext(ctx, net.JoinHostPort(addr, port), grpc.WithTransportCredentials(credentials.NewTLS(kp.TLSConfigForMasterClient())), grpc.WithUnaryInterceptor(rpcz.UnaryClientInterceptor), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("failed to connect to discovery server: %v", err)
	}
	client := pb.NewDiscoveryServiceClient(conn)

	// Enable gRPC-over-UDP
	udpSocket, err := udptransport.New(HandleIncomingContentConnection, net.JoinHostPort(addr, port))
	if err != nil {
		log.Printf("failed to enable gRPC-over-UDP: %v", err)
	}

	c := &circle{
		name:        name,
		client:      client,
		myPort:      myPort,
		udpSocket:   udpSocket,
		myEndpoints: myEndpoints,
		keyPair:     kp,
		peers:       map[string]*Peer{},
	}

	go c.run(ctx)
	cmtx.Lock()
	circles[name] = c
	cmtx.Unlock()
	remotelogging.AddSink(client)
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
	var endpoints []*pb.Endpoint

	if len(c.myEndpoints) > 0 {
		for _, endpoint := range c.myEndpoints {
			_, _, err := net.SplitHostPort(endpoint)
			if err != nil {
				endpoint = net.JoinHostPort(endpoint, fmt.Sprint(c.myPort))
			}
			endpoints = append(endpoints, &pb.Endpoint{
				Type:    pb.Endpoint_TCP,
				Address: endpoint,
			})
		}
	} else {
		// Auto-detect endpoints
		res, err := c.client.GetMyIP(ctx, &pb.GetMyIPRequest{})
		if err != nil {
			return fmt.Errorf("no ips given and failed to retrieve IP from discovery server")
		}
		endpoints = append(endpoints, &pb.Endpoint{
			Type:    pb.Endpoint_TCP,
			Address: net.JoinHostPort(res.GetIp(), fmt.Sprint(c.myPort)),
		})
	}

	// Auto-detect our gRPC-over-SCTP-over-UDP public endpoint
	udpEndpoint, err := c.udpSocket.PerformStunlite(ctx)
	if err != nil {
		log.Printf("gRPC-over-UDP disabled, stunlite failed: %v", err)
	} else {
		endpoints = append(endpoints, &pb.Endpoint{
			Type:    pb.Endpoint_SCTP_OVER_UDP,
			Address: udpEndpoint,
		})
	}

	// TODO: Remove this, as soon as all Discovery servers have new-endpoint support.
	var oldEndpoints []string
	for _, endpoint := range endpoints {
		if endpoint.GetType() == pb.Endpoint_TCP {
			oldEndpoints = append(oldEndpoints, endpoint.GetAddress())
		}
	}

	stream, err := c.client.Connect(ctx, &pb.ConnectRequest{
		OldEndpoints:  oldEndpoints,
		ClientVersion: version.GetVersion(),
		Endpoints:     endpoints,
	}, grpc.WaitForReady(true))
	if err != nil {
		return fmt.Errorf("failed to subscribe to discovery server: %v", err)
	}
	log.Printf("Connected to RuFS circle %s. My endpoints: %v", c.name, endpoints)
	go runConnectivityMetrics(ctx, c.name, c.client)

	for {
		msg, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("discovery server stream error: %v", err)
		}
		if msg.GetPeerList() != nil {
			c.processPeers(ctx, msg.GetPeerList().GetPeers())
		}
		if msg.GetResolveConflictRequest() != nil {
			HandleResolveConflictRequest(ctx, msg.GetResolveConflictRequest(), c.name)
		}
		if msg.GetActiveDownloads() != nil {
			HandleActiveDownloadList(ctx, msg.GetActiveDownloads(), c.name)
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
	for name, cp := range c.peers {
		found := false
		for _, pe := range peers {
			if pe.GetName() == name {
				found = true
				break
			}
		}
		if !found {
			cp.conn.Close()
			delete(c.peers, name)
		}
	}
}

func (c *circle) newPeer(ctx context.Context, p *pb.Peer) *Peer {
	nextManualResolverScheme++
	r := manual.NewBuilderWithScheme(fmt.Sprintf("rufs-%d", nextManualResolverScheme))
	r.InitialState(c.peerToResolverState(p))

	// If we enable keepalive on a peer that does not allow it, they will
	// close the connection with a GOAWAY / ENHANCE_YOUR_CALM error.
	// TODO: Once all clients support this, remove this and assume all
	// peers are keepaliveable.
	isKeepaliveableClient := false
	for _, endpoint := range p.GetEndpoints() {
		if endpoint.Type != pb.Endpoint_TCP {
			isKeepaliveableClient = true
		}
	}

	var keepaliveOption grpc.DialOption
	if isKeepaliveableClient {
		keepaliveOption = grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                time.Second * 60,
			Timeout:             time.Second * 30,
			PermitWithoutStream: true,
		})
	} else {
		keepaliveOption = grpc.EmptyDialOption{}
	}

	conn, err := grpc.DialContext(
		ctx,
		r.Scheme()+":///magic",
		grpc.WithResolvers(r),
		grpc.WithContextDialer(c.dialPeer),
		grpc.WithTransportCredentials(credentials.NewTLS(c.keyPair.TLSConfigForServerClient(p.GetName()))),
		keepaliveOption,
		// gRPC should keep a channel open to all endpoints, since UDP hole-punching
		// between two clients may only work if they are trying to connect to each
		// other via UDP. Hence, even if one client can connect to the other via TCP,
		// and this method of communication is preferred, the client should still
		// also connect using UDP; if not, the other client may not be able to
		// connect to the first client at all.
		grpc.WithDisableServiceConfig(),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [ { "`+BalancerName+`": {} } ]}`),
		grpc.WithUnaryInterceptor(rpcz.UnaryClientInterceptor),
	)
	if err != nil {
		log.Fatalf("Failed to dial peer %q: %v", r.Scheme(), err)
	}
	return &Peer{
		Name:     p.GetName(),
		circle:   c,
		conn:     conn,
		resolver: r,
	}
}

func (c *circle) myName() string {
	return c.keyPair.CommonName()
}

func (c *circle) dialPeer(ctx context.Context, addr string) (net.Conn, error) {
	endpointType, addr := splitGrpcAddress(addr)
	switch endpointType {
	case pb.Endpoint_SCTP_OVER_UDP:
		return c.udpSocket.DialContext(ctx, addr)
	case pb.Endpoint_TCP:
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	default:
		return nil, fmt.Errorf("internal error: unknown endpoint type in address: %s", addr)
	}
}

func joinGrpcAddress(t pb.Endpoint_Type, addr string) string {
	return t.String() + ":" + addr
}

func splitGrpcAddress(addr string) (pb.Endpoint_Type, string) {
	sp := strings.SplitN(addr, ":", 2)
	if v, ok := pb.Endpoint_Type_value[sp[0]]; ok {
		return pb.Endpoint_Type(v), sp[1]
	}
	return pb.Endpoint_UNKNOWN_TYPE, sp[1]
}

func (c *circle) peerToResolverState(p *pb.Peer) resolver.State {
	var s resolver.State

	if p.GetName() == c.myName() {
		// Connecting to myself
		s.Addresses = append(s.Addresses, resolver.Address{
			Addr:       fmt.Sprintf("%s:127.0.0.1:%d", pb.Endpoint_TCP.String(), c.myPort),
			ServerName: c.myName(),
		})
		return s
	}

	for _, e := range p.GetEndpoints() {
		if _, known := pb.Endpoint_Type_name[int32(e.Type)]; !known || e.Type == 0 {
			continue
		}
		s.Addresses = append(s.Addresses, resolver.Address{
			Addr:       joinGrpcAddress(e.Type, e.GetAddress()),
			ServerName: p.GetName(),
		})
	}

	// TODO: remove this once all Discoveries send the new endpoint list
	if len(p.GetEndpoints()) == 0 {
		for _, e := range p.GetOldEndpoints() {
			s.Addresses = append(s.Addresses, resolver.Address{
				Addr:       pb.Endpoint_TCP.String() + ":" + e,
				ServerName: p.GetName(),
			})
		}
	}

	return s
}

type Peer struct {
	Name     string
	circle   *circle
	conn     *grpc.ClientConn
	resolver *manual.Resolver
}

func (p *Peer) update(pe *pb.Peer) {
	p.resolver.UpdateState(p.circle.peerToResolverState(pe))
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

func AllDiscoveryClients() []pb.DiscoveryServiceClient {
	cmtx.Lock()
	defer cmtx.Unlock()
	ret := make([]pb.DiscoveryServiceClient, 0, len(circles))
	for _, c := range circles {
		ret = append(ret, c.client)
	}
	return ret
}

func CirclesFromPeers(peers []*Peer) []string {
	cs := map[string]bool{}
	for _, p := range peers {
		cs[common.CircleFromPeer(p.Name)] = true
	}
	ret := make([]string, 0, len(cs))
	for c := range cs {
		ret = append(ret, c)
	}
	return ret
}

func WaitForCircle(ctx context.Context, name string) error {
	for {
		cmtx.Lock()
		c, found := circles[name]
		cmtx.Unlock()
		if found && len(c.AllPeers()) > 0 {
			return c.WaitForPeers(ctx)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (c *circle) WaitForPeers(ctx context.Context) error {
	for _, p := range c.AllPeers() {
		for {
			ls := p.conn.GetState()
			if ls == connectivity.Ready {
				break
			}
			if !p.conn.WaitForStateChange(ctx, ls) {
				return ctx.Err()
			}
		}
	}
	return nil
}
