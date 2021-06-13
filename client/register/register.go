package register

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/sgielen/rufs/client/config"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"github.com/sgielen/rufs/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func Register(ctx context.Context, circleAddr, username, token, caFingerprint string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	circle, discoveryPort, err := net.SplitHostPort(circleAddr)
	if err != nil {
		circle = circleAddr
		discoveryPort = "12000"
	}

	tc, caPromise := security.TLSConfigForRegistration(circle, caFingerprint)

	conn, err := grpc.DialContext(ctx, net.JoinHostPort(circle, discoveryPort), grpc.WithTransportCredentials(credentials.NewTLS(tc)), grpc.WithReturnConnectionError(), grpc.FailOnNonTempDialError(true))
	if err != nil {
		return fmt.Errorf("failed to connect to discovery server: %v", err)
	}
	defer conn.Close()
	c := pb.NewDiscoveryServiceClient(conn)

	key, err := security.NewKey()
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %v", err)
	}
	pub, err := key.SerializePublicKey()
	if err != nil {
		return fmt.Errorf("failed to serialize public key: %v", err)
	}

	resp, err := c.Register(ctx, &pb.RegisterRequest{
		Username:      username,
		Token:         token,
		PublicKey:     pub,
		ClientVersion: version.GetVersion(),
	})
	if err != nil {
		return fmt.Errorf("failed to register with circle: %v", err)
	}

	caFile, crtFile, keyFile := config.PKIFiles(circle)
	if err := os.MkdirAll(filepath.Dir(caFile), 0755); err != nil {
		return fmt.Errorf("Failed to create PKI directory %q: %v", filepath.Dir(caFile), err)
	}

	if err := key.StorePrivateKey(keyFile); err != nil {
		return fmt.Errorf("failed to store private key: %v", err)
	}

	if err := ioutil.WriteFile(crtFile, resp.GetCertificate(), 0644); err != nil {
		os.Remove(keyFile)
		return fmt.Errorf("failed to store certificate: %v", err)
	}

	if err := ioutil.WriteFile(caFile, *caPromise, 0644); err != nil {
		os.Remove(keyFile)
		os.Remove(crtFile)
		return fmt.Errorf("failed to store CA certificate: %v", err)
	}
	return nil
}
