package httpserver

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewH3ServerLoadsTLSConfig(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeH3LocalhostCertificate(t)
	server, err := NewH3Server("127.0.0.1:0", http.NotFoundHandler(), certFile, keyFile, "test.h3")
	if err != nil {
		t.Fatalf("NewH3Server() error = %v", err)
	}

	if server.Addr != "127.0.0.1:0" {
		t.Fatalf("Addr = %q, want 127.0.0.1:0", server.Addr)
	}
	if server.TLSConfig == nil {
		t.Fatal("TLSConfig = nil")
	}
	if server.TLSConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("MinVersion = %x, want %x", server.TLSConfig.MinVersion, tls.VersionTLS13)
	}
	if len(server.TLSConfig.Certificates) != 1 {
		t.Fatalf("Certificates length = %d, want 1", len(server.TLSConfig.Certificates))
	}
	if server.QUICConfig == nil {
		t.Fatal("QUICConfig = nil")
	}
	if server.QUICConfig.InitialPacketSize != 1200 {
		t.Fatalf("InitialPacketSize = %d, want 1200", server.QUICConfig.InitialPacketSize)
	}
	if server.MaxHeaderBytes != http.DefaultMaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, http.DefaultMaxHeaderBytes)
	}
}

func writeH3LocalhostCertificate(t *testing.T) (value0, value1 string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "h3.crt")
	keyFile := filepath.Join(dir, "h3.key")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return certFile, keyFile
}
