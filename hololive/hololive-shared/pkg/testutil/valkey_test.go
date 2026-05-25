package testutil

import (
	"context"
	"testing"
	"time"
)

func TestNewTestValkeyClient(t *testing.T) {
	client, mini := NewTestValkeyClient(t)

	if client == nil {
		t.Fatal("client is nil")
	}
	if mini == nil {
		t.Fatal("miniredis is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := client.Do(ctx, client.B().Set().Key("test:key").Value("ok").Build()).Error(); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, err := client.Do(ctx, client.B().Get().Key("test:key").Build()).ToString()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "ok" {
		t.Fatalf("value mismatch: got %q want %q", got, "ok")
	}
}
