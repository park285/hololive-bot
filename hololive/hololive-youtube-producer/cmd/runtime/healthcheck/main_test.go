package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func TestHTTP3HealthClientUsesConstrainedInitialPacketSize(t *testing.T) {
	client, closeFn, err := newHTTP3HealthClient()
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()

	transport, ok := client.Transport.(*http3.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http3.Transport", client.Transport)
	}
	if transport.QUICConfig == nil {
		t.Fatal("QUICConfig = nil")
	}
	if transport.QUICConfig.InitialPacketSize != 1200 {
		t.Fatalf("InitialPacketSize = %d, want 1200", transport.QUICConfig.InitialPacketSize)
	}
	if transport.QUICConfig.DisablePathMTUDiscovery {
		t.Fatal("DisablePathMTUDiscovery = true, want PMTUD enabled")
	}
}

func TestParseURLRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "missing scheme", raw: "localhost:3000/health"},
		{name: "unsupported scheme", raw: "ftp://localhost/health"},
		{name: "missing host", raw: "https:///health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseURL(tt.raw); err == nil {
				t.Fatalf("parseURL(%q) error = nil, want error", tt.raw)
			}
		})
	}
}

func TestCheckURLAcceptsHTTP3LoopbackWithServerNameOverride(t *testing.T) {
	certFile, keyFile := writeHealthcheckCert(t, "hololive-h3.local")
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load cert: %v", err)
	}

	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	server := &http3.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				t.Fatalf("path = %q, want /health", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}),
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
		},
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	t.Setenv("HEALTHCHECK_CA_CERT_FILE", certFile)
	t.Setenv("HEALTHCHECK_SERVER_NAME", "hololive-h3.local")

	url := "https://" + listener.LocalAddr().String() + "/health"
	if err := checkURL(url); err != nil {
		t.Fatalf("checkURL(%q): %v", url, err)
	}
}

func writeHealthcheckCert(t *testing.T, serverName string) (string, string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: serverName},
		DNSNames:              []string{serverName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("create cert file: %v", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := certOut.Close(); err != nil {
		t.Fatalf("close cert: %v", err)
	}

	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := keyOut.Close(); err != nil {
		t.Fatalf("close key: %v", err)
	}

	return certFile, keyFile
}
