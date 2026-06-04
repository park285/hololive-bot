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
	appConfig := &config.Config{
		Server: config.ServerConfig{
			H3Addr:     "127.0.0.1:0",
			H3CertFile: certFile,
			H3KeyFile:  keyFile,
		},
	}

	server, err := BuildBotHTTP3Server(t.Context(), appConfig, nil, nil, nil)
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
	if server.TLSConfig.GetCertificate == nil {
		t.Fatal("GetCertificate = nil")
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

func TestBuildBotHTTP3ServerServesCachedCertificateFiles(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeLocalhostCertificate(t)
	appConfig := &config.Config{
		Server: config.ServerConfig{
			H3Addr:     "127.0.0.1:0",
			H3CertFile: certFile,
			H3KeyFile:  keyFile,
		},
	}

	server, err := BuildBotHTTP3Server(t.Context(), appConfig, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildBotHTTP3Server() error = %v", err)
	}

	if server.TLSConfig.GetCertificate == nil {
		t.Fatal("GetCertificate = nil")
	}

	first, err := server.TLSConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("first GetCertificate() error = %v", err)
	}

	firstSerial := certificateSerial(t, first)

	overwriteLocalhostCertificate(t, certFile, keyFile)

	second, err := server.TLSConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("second GetCertificate() error = %v", err)
	}

	secondSerial := certificateSerial(t, second)

	if secondSerial != firstSerial {
		t.Fatalf("certificate serial = %s, want cached %s", secondSerial, firstSerial)
	}
}

func TestBuildBotHTTP3ServerKeepsPreviousCertificateWhenReloadFails(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeLocalhostCertificate(t)
	appConfig := &config.Config{
		Server: config.ServerConfig{
			H3Addr:     "127.0.0.1:0",
			H3CertFile: certFile,
			H3KeyFile:  keyFile,
		},
	}

	server, err := BuildBotHTTP3Server(t.Context(), appConfig, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildBotHTTP3Server() error = %v", err)
	}

	if server.TLSConfig.GetCertificate == nil {
		t.Fatal("GetCertificate = nil")
	}

	first, err := server.TLSConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("first GetCertificate() error = %v", err)
	}

	firstSerial := certificateSerial(t, first)

	writeErr := os.WriteFile(keyFile, []byte("not a private key"), 0o600)
	if writeErr != nil {
		t.Fatalf("write invalid key: %v", writeErr)
	}

	second, err := server.TLSConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("second GetCertificate() error = %v", err)
	}

	secondSerial := certificateSerial(t, second)

	if secondSerial != firstSerial {
		t.Fatalf("certificate serial = %s, want previous %s", secondSerial, firstSerial)
	}
}

func writeLocalhostCertificate(t *testing.T) (string, string) {
	t.Helper()

	certPEM, keyPEM := generateLocalhostCertificate(t)
	dir := t.TempDir()
	certFile := filepath.Join(dir, "localhost.crt")
	keyFile := filepath.Join(dir, "localhost.key")

	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return certFile, keyFile
}

func overwriteLocalhostCertificate(t *testing.T, certFile, keyFile string) {
	t.Helper()

	certPEM, keyPEM := generateLocalhostCertificate(t)
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write replacement cert: %v", err)
	}

	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write replacement key: %v", err)
	}
}

func generateLocalhostCertificate(t *testing.T) ([]byte, []byte) {
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

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func certificateSerial(t *testing.T, cert *tls.Certificate) string {
	t.Helper()

	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatal("certificate chain is empty")
	}

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	return parsed.SerialNumber.String()
}
