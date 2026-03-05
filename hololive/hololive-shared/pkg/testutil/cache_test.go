package testutil

import (
	"context"
	"testing"
	"time"
)

type wrapperCachePayload struct {
	Value string `json:"value"`
}

func TestNewTestCacheServiceWithMini(t *testing.T) {
	ctx := context.Background()
	svc, mini := NewTestCacheServiceWithMini(t, ctx)

	if svc == nil {
		t.Fatal("service is nil")
	}
	if mini == nil {
		t.Fatal("miniredis is nil")
	}

	in := wrapperCachePayload{Value: "wrapper-ok"}
	if err := svc.Set(ctx, "wrapper:test:key", in, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}

	var out wrapperCachePayload
	if err := svc.Get(ctx, "wrapper:test:key", &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Value != in.Value {
		t.Fatalf("value mismatch: got %q want %q", out.Value, in.Value)
	}
}

func TestNewTestCacheService(t *testing.T) {
	ctx := context.Background()
	svc := NewTestCacheService(t, ctx)
	if svc == nil {
		t.Fatal("service is nil")
	}
}
