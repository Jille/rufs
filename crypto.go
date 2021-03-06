package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

type MasterVault struct {
	ca   *x509.Certificate
	priv *rsa.PrivateKey
}

func (mv *MasterVault) getTlsCert() *tls.Certificate {
	return &tls.Certificate{
		Certificate: [][]byte{mv.ca.Raw},
		PrivateKey:  mv.priv,
		Leaf:        mv.ca,
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

func createCA(dir string, name string) error {
	keyfn := filepath.Join(dir, "ca.key")
	t := createCertTemplate(true, name)
	var err error
	priv, err := createKeyPair(keyfn)
	if err != nil {
		return err
	}
	pub := &priv.PublicKey
	ca, err := x509.CreateCertificate(rand.Reader, t, t, pub, priv)
	if err != nil {
		os.Remove(keyfn)
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

func loadMasterVault(dir string) (*MasterVault, error) {
	mv := &MasterVault{}
	var err error
	mv.ca, err = loadCertificate(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	priv, err := pemFromFile(filepath.Join(dir, "ca.key"), "RSA PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	mv.priv, err = x509.ParsePKCS1PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return mv, nil
}

func createKeyPair(fn string) (*rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	mp := x509.MarshalPKCS1PrivateKey(priv)

	if err := pemToFile(fn, "RSA PRIVATE KEY", mp, 0600); err != nil {
		return nil, err
	}

	return priv, nil
}

func serializePubKey(priv *rsa.PrivateKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(&priv.PublicKey)
}

func signClient(mv *MasterVault, pub []byte, name string) ([]byte, error) {
	pk, err := x509.ParsePKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}

	t := createCertTemplate(false, name)
	cert, err := x509.CreateCertificate(rand.Reader, t, mv.ca, pk, mv.priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), nil
}

func createAuthToken(mv *MasterVault, user string) string {
	h := sha1.New()
	h.Write([]byte(user))
	h.Write([]byte{0})
	h.Write(x509.MarshalPKCS1PrivateKey(mv.priv))
	return fmt.Sprintf("%x", h.Sum(nil))
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
	pem, _ := pem.Decode(data)
	if pem == nil {
		return nil, fmt.Errorf("error when reading %q: no PEM block found", fn)
	}
	if pem.Type != pemType {
		return nil, fmt.Errorf("error when reading %q: expected PEM type %q, found %q", fn, pemType, pem.Type)
	}
	return pem.Bytes, nil
}

type tlsConfigType int

const (
	TlsConfigMaster tlsConfigType = iota
	TlsConfigMasterClient
	TlsConfigServer
	TlsConfigServerClient
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
	case TlsConfigMaster, TlsConfigMasterClient:
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
		cfg.PreferServerCipherSuites = true
	case TlsConfigServer, TlsConfigServerClient:
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg
}
