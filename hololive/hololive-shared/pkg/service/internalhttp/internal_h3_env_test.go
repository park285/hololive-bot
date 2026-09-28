package internalhttp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestCACertificate(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "hololive-internal-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}

	return path
}

// 내부 H3 client는 전용 HOLOLIVE_INTERNAL_H3_* 두 키만 쓴다. 공통 서버 키(HOLOLIVE_H3_CERT_FILE,
// HOLOLIVE_H3_SERVER_NAME)로 내려가는 폴백은 두지 않는다(stack audit 2026-09-26).
func TestNewClientForURLStrictRequiresInternalH3EnvWithoutServerFallback(t *testing.T) {
	serverCert := writeTestCACertificate(t)

	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", "")
	t.Setenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME", "")
	t.Setenv("HOLOLIVE_H3_CERT_FILE", serverCert)
	t.Setenv("HOLOLIVE_H3_SERVER_NAME", "127.0.0.1")

	if client, err := NewClientForURLStrict("https://hololive-admin-api:30006", time.Second, nil); err == nil || client != nil {
		t.Fatalf("NewClientForURLStrict() = (%T, %v), want missing internal H3 env error", client, err)
	}

	t.Setenv("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", serverCert)
	t.Setenv("HOLOLIVE_INTERNAL_H3_SERVER_NAME", "127.0.0.1")

	client, err := NewClientForURLStrict("https://hololive-admin-api:30006", time.Second, nil)
	if err != nil || client == nil {
		t.Fatalf("NewClientForURLStrict() = (%T, %v), want configured client", client, err)
	}

	if err := CloseClient(client); err != nil {
		t.Errorf("CloseClient() error = %v", err)
	}
}
