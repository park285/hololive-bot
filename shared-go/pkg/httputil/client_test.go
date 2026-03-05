package httputil

import (
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	const timeout = 15 * time.Second
	client := NewClient(timeout)
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.Timeout != timeout {
		t.Fatalf("NewClient() timeout = %s, want %s", client.Timeout, timeout)
	}
}

func TestDefaultClient(t *testing.T) {
	t.Parallel()

	client := DefaultClient()
	if client == nil {
		t.Fatal("DefaultClient() returned nil")
	}

	const wantTimeout = 30 * time.Second
	if client.Timeout != wantTimeout {
		t.Fatalf("DefaultClient() timeout = %s, want %s", client.Timeout, wantTimeout)
	}
}
