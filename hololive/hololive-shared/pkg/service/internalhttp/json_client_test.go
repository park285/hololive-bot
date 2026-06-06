package internalhttp

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNewClientForURLStrictReturnsErrorWhenH3ClientConfigFails(t *testing.T) {
	t.Setenv(internalH3CACertFileEnv, filepath.Join(t.TempDir(), "missing-ca.pem"))

	client, err := NewClientForURLStrict("https://hololive-admin-api:30006", time.Second, nil)
	if err == nil {
		t.Fatal("NewClientForURLStrict() error = nil, want h3 client config error")
	}
	if client != nil {
		t.Fatalf("NewClientForURLStrict() client = %T, want nil on error", client)
	}
}

func TestNewClientForURLStrictKeepsPlainHTTPClient(t *testing.T) {
	t.Setenv(internalH3CACertFileEnv, filepath.Join(t.TempDir(), "missing-ca.pem"))

	client, err := NewClientForURLStrict("http://localhost:30190", time.Second, nil)
	if err != nil {
		t.Fatalf("NewClientForURLStrict(http) error = %v", err)
	}
	if client == nil {
		t.Fatal("NewClientForURLStrict(http) client = nil")
	}
}
