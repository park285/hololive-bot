package internalhttp

import (
	"path/filepath"
	"testing"
	"time"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
)

func TestNewClientForURLStrictReturnsErrorWhenH3ClientConfigFails(t *testing.T) {
	options := sharedh3.ClientOptions{CACertFile: filepath.Join(t.TempDir(), "missing-ca.pem"), ServerName: testInternalH3ServerName}

	client, err := NewClientForURLStrict("https://hololive-admin-api:30006", time.Second, options)
	if err == nil {
		t.Fatal("NewClientForURLStrict() error = nil, want h3 client config error")
	}

	if client != nil {
		t.Fatalf("NewClientForURLStrict() client = %T, want nil on error", client)
	}
}

func TestNewClientForURLStrictKeepsPlainHTTPClient(t *testing.T) {
	options := sharedh3.ClientOptions{CACertFile: filepath.Join(t.TempDir(), "missing-ca.pem"), ServerName: testInternalH3ServerName}

	client, err := NewClientForURLStrict("http://localhost:30190", time.Second, options)
	if err != nil {
		t.Fatalf("NewClientForURLStrict(http) error = %v", err)
	}

	if client == nil {
		t.Fatal("NewClientForURLStrict(http) client = nil")
	}
}
