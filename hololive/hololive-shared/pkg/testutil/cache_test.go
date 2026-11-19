package testutil

import (
	"testing"
	"time"
)

type wrapperCachePayload struct {
	Value string `json:"value"`
}

func TestNewTestCacheServiceWithMini(t *testing.T) {
	ctx := t.Context()
	service, mini := NewTestCacheServiceWithMini(ctx, t)

	if service == nil {
		t.Fatal("service is nil")
	}

	if mini == nil {
		t.Fatal("miniredis is nil")
	}

	in := wrapperCachePayload{Value: "wrapper-ok"}
	if err := service.Set(ctx, "wrapper:test:key", in, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}

	var out wrapperCachePayload

	if err := service.Get(ctx, "wrapper:test:key", &out); err != nil {
		t.Fatalf("get: %v", err)
	}

	if out.Value != in.Value {
		t.Fatalf("value mismatch: got %q want %q", out.Value, in.Value)
	}
}

func TestNewTestCacheService(t *testing.T) {
	ctx := t.Context()
	service := NewTestCacheService(ctx, t)

	if service == nil {
		t.Fatal("service is nil")
	}
}
