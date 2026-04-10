package mocks

import (
	"context"
	"testing"
)

func TestClientCloseDefaultsToNoopWhenNotStrict(t *testing.T) {
	var client Client

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestClientIsConnectedDefaultsToFalseWhenNotStrict(t *testing.T) {
	var client Client

	if client.IsConnected(context.Background()) {
		t.Fatal("IsConnected() = true, want false")
	}
}

func TestClientClosePanicsWhenStrict(t *testing.T) {
	client := Client{Strict: true}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()

	_ = client.Close()
}
