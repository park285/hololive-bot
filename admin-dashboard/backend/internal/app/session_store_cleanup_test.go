package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/cleanupctx"
)

type cleanupSessionContextKey string

func TestCleanupSessionStoreDeleteDetachesCancellation(t *testing.T) {
	var gotErr error
	var gotValue any
	var gotDeadline time.Time
	underlying := &fakeSessions{deleteFn: func(ctx context.Context, _ string) error {
		gotErr = ctx.Err()
		gotValue = ctx.Value(cleanupSessionContextKey("trace"))
		gotDeadline, _ = ctx.Deadline()
		return nil
	}}
	store := newCleanupSessionStore(underlying)

	parent := context.WithValue(context.Background(), cleanupSessionContextKey("trace"), "trace-1")
	parent, cancelParent := context.WithCancel(parent)
	cancelParent()

	if err := store.Delete(parent, "session-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if gotErr != nil {
		t.Fatalf("underlying delete context error = %v, want nil", gotErr)
	}
	if gotValue != "trace-1" {
		t.Fatalf("underlying delete context value = %v, want trace-1", gotValue)
	}
	remaining := time.Until(gotDeadline)
	if remaining <= 0 || remaining > cleanupctx.DefaultTimeout+time.Second {
		t.Fatalf("cleanup deadline remaining = %v", remaining)
	}
}

func TestCleanupSessionStoreDeletePreservesError(t *testing.T) {
	wantErr := errors.New("delete failed")
	store := newCleanupSessionStore(&fakeSessions{deleteFn: func(context.Context, string) error {
		return wantErr
	}})
	if err := store.Delete(context.Background(), "session-1"); !errors.Is(err, wantErr) {
		t.Fatalf("Delete() error = %v, want %v", err, wantErr)
	}
}
