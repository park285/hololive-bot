package bootstrap

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestBuildBotHTTP3ServerLoadsTLSConfig(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeLocalhostCertificate(t)
	cfg := &config.Config{
		Server: config.ServerConfig{
			H3Addr:     "127.0.0.1:0",
			H3CertFile: certFile,
			H3KeyFile:  keyFile,
		},
	}

	server, err := BuildBotHTTP3Server(t.Context(), cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildBotHTTP3Server() error = %v", err)
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
		t.Fatalf("Certificates len = %d, want 1", len(server.TLSConfig.Certificates))
	}
	if server.QUICConfig == nil {
		t.Fatal("QUICConfig = nil")
	}
	if server.QUICConfig.InitialPacketSize != 1200 {
		t.Fatalf("InitialPacketSize = %d, want 1200", server.QUICConfig.InitialPacketSize)
	}
	if server.QUICConfig.DisablePathMTUDiscovery {
		t.Fatal("DisablePathMTUDiscovery = true, want default PMTUD enabled")
	}
}

func writeLocalhostCertificate(t *testing.T) (string, string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}

	cert := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "localhost.crt")
	keyFile := filepath.Join(dir, "localhost.key")

	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return certFile, keyFile
}
