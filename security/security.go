// Package security handles all the TLS stuff.
package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// CAKeyPair holds a CA certificate and private key.
type CAKeyPair struct {
	ca   *x509.Certificate
	priv *rsa.PrivateKey
}

// LoadCAKeyPair loads ca.rt and ca.key from $dir.
func LoadCAKeyPair(dir string) (*CAKeyPair, error) {
	p := &CAKeyPair{}
	var err error
	p.ca, err = loadCertificate(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	priv, err := pemFromFile(filepath.Join(dir, "ca.key"), "RSA PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	p.priv, err = x509.ParsePKCS1PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Name returns the CommonName of the CA. (The circle name.)
func (p *CAKeyPair) Name() string {
	return p.ca.Subject.CommonName
}

// Sign a given public key with this CA and create a certificate for $name.
func (p *CAKeyPair) Sign(pubKey []byte, name string) ([]byte, error) {
	pk, err := x509.ParsePKIXPublicKey(pubKey)
	if err != nil {
		return nil, err
	}

	t := createCertTemplate(false, name)
	cert, err := x509.CreateCertificate(rand.Reader, t, p.ca, pk, p.priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), nil
}

// CreateToken creates a registration token for $user.
func (p *CAKeyPair) CreateToken(user string) string {
	h := sha1.New()
	h.Write([]byte(user))
	h.Write([]byte{0})
	h.Write(x509.MarshalPKCS1PrivateKey(p.priv))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (p *CAKeyPair) TLSConfigForDiscovery() *tls.Config {
	return getTlsConfig(tlsConfigMaster, p.ca, p.certificate(), "rufs-master")
}

// certificate converts this pair to a *tls.Certificate.
func (p *CAKeyPair) certificate() *tls.Certificate {
	return &tls.Certificate{
		Certificate: [][]byte{p.ca.Raw},
		PrivateKey:  p.priv,
		Leaf:        p.ca,
	}
}

func createCertTemplate(isCA bool, name string) *x509.Certificate {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		panic(fmt.Errorf("failed to generate serial number: %s", err))
	}

	cert := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   name,
			Organization: []string{"RUFS"},
		},
		DNSNames:              []string{name},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	if isCA {
		cert.IsCA = true
		cert.KeyUsage |= x509.KeyUsageCertSign
	}
	return cert
}

// NewCA creates a new CA key pair and writes it to disk.
func NewCA(dir string, name string) error {
	t := createCertTemplate(true, name)
	var err error
	ks, err := NewKey()
	if err != nil {
		return err
	}
	pub := &ks.priv.PublicKey
	ca, err := x509.CreateCertificate(rand.Reader, t, t, pub, ks.priv)
	if err != nil {
		return err
	}

	keyfn := filepath.Join(dir, "ca.key")
	if err := ks.StorePrivateKey(keyfn); err != nil {
		return err
	}
	if err := pemToFile(filepath.Join(dir, "ca.crt"), "CERTIFICATE", ca, 0644); err != nil {
		os.Remove(keyfn)
		return err
	}
	return nil
}

func loadCertificate(fn string) (*x509.Certificate, error) {
	ca, err := pemFromFile(fn, "CERTIFICATE")
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(ca)
}

// KeySerializer holds a new key pair and can serialize it.
type KeySerializer struct {
	priv *rsa.PrivateKey
}

// NewKey generates a new key pair.
func NewKey() (KeySerializer, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return KeySerializer{}, err
	}
	return KeySerializer{priv}, nil
}

// StorePrivateKey writes the private key to disk.
func (s KeySerializer) StorePrivateKey(fn string) error {
	return pemToFile(fn, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(s.priv), 0600)
}

// SerializePublicKey returns the encoded public key.
func (s KeySerializer) SerializePublicKey() ([]byte, error) {
	return x509.MarshalPKIXPublicKey(&s.priv.PublicKey)
}

func pemToFile(fn, pemType string, data []byte, mode os.FileMode) error {
	fh, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if err := pem.Encode(fh, &pem.Block{Type: pemType, Bytes: data}); err != nil {
		fh.Close()
		os.Remove(fn)
		return err
	}
	if err := fh.Close(); err != nil {
		os.Remove(fn)
		return err
	}
	return nil
}

func pemFromFile(fn, pemType string) ([]byte, error) {
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, fmt.Errorf("error when reading %q: %v", fn, err)
	}
	p, err := decodePem(data, pemType)
	if err != nil {
		return nil, fmt.Errorf("error when decoding %q: %v", fn, err)
	}
	return p, nil
}

func decodePem(data []byte, pemType string) ([]byte, error) {
	pem, _ := pem.Decode(data)
	if pem == nil {
		return nil, errors.New("no PEM block found")
	}
	if pem.Type != pemType {
		return nil, fmt.Errorf("expected PEM type %q, found %q", pemType, pem.Type)
	}
	return pem.Bytes, nil
}

type tlsConfigType int

const (
	tlsConfigMaster tlsConfigType = iota
	tlsConfigMasterClient
	tlsConfigServer
	tlsConfigServerClient
)

func getTlsConfig(mode tlsConfigType, ca *x509.Certificate, cert *tls.Certificate, serverName string) *tls.Config {
	CAs := x509.NewCertPool()
	CAs.AddCert(ca)
	cfg := &tls.Config{
		RootCAs:    CAs,
		ClientCAs:  CAs,
		ServerName: serverName,
	}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	switch mode {
	case tlsConfigMaster, tlsConfigMasterClient:
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
		cfg.PreferServerCipherSuites = true
	case tlsConfigServer, tlsConfigServerClient:
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg
}

func TLSConfigForRegistration(caPem []byte) (*tls.Config, error) {
	ca, err := parseCertificate(caPem)
	if err != nil {
		return nil, err
	}
	return getTlsConfig(tlsConfigMasterClient, ca, nil, ca.Subject.CommonName), nil
}

func parseCertificate(crtPEM []byte) (*x509.Certificate, error) {
	crt, err := decodePem(crtPEM, "CERTIFICATE")
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(crt)
}

func LoadKeyPair(caPEM, crtPEM, keyPEM []byte) (*KeyPair, error) {
	ca, err := parseCertificate(caPEM)
	if err != nil {
		return nil, err
	}
	crt, err := tls.X509KeyPair(crtPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	x509Cert, err := x509.ParseCertificate(crt.Certificate[0])
	if err != nil {
		return nil, err
	}
	crt.Leaf = x509Cert
	return &KeyPair{
		ca:  ca,
		crt: crt,
	}, nil
}

type KeyPair struct {
	ca  *x509.Certificate
	crt tls.Certificate
}

func (p *KeyPair) TLSConfigForMasterClient() *tls.Config {
	return getTlsConfig(tlsConfigMasterClient, p.ca, &p.crt, p.ca.Subject.CommonName)
}

func (p *KeyPair) TLSConfigForServer() *tls.Config {
	return getTlsConfig(tlsConfigServer, p.ca, &p.crt, p.crt.Leaf.Subject.CommonName)
}

func (p *KeyPair) TLSConfigForServerClient(name string) *tls.Config {
	return getTlsConfig(tlsConfigServer, p.ca, &p.crt, name)
}

// PeerFromContext can be called from inside an RPC handler to get the remote peer and circle name.
func PeerFromContext(ctx context.Context) (name string, circle string, err error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		// This should never happen.
		return "", "", status.Error(codes.Unauthenticated, "no Peer attached to context; TLS issue?")
	}
	ti, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", "", status.Error(codes.Unauthenticated, "couldn't get TLSInfo; TLS issue?")
	}
	if len(ti.State.PeerCertificates) == 0 {
		return "", "", status.Error(codes.Unauthenticated, "no client certificate given")
	}
	c := ti.State.PeerCertificates[0]
	return c.Subject.CommonName, c.Issuer.CommonName, nil
}
