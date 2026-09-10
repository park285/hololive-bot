package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/cleanupctx"
)

type cleanupSessionContextKey string

func TestCleanupSessionStoreDeleteDetachesCancellation(t *testing.T) {
	var (
		gotErr      error
		gotValue    any
		gotDeadline time.Time
	)

	underlying := &fakeSessions{deleteFn: func(ctx context.Context, _ string) error {
		gotErr = ctx.Err()
		gotValue = ctx.Value(cleanupSessionContextKey("trace"))
		gotDeadline, _ = ctx.Deadline()

		return nil
	}}
	store := newCleanupSessionStore(underlying)

	if store == nil {
		t.Fatal("newCleanupSessionStore() returned nil")
	}

	parent := context.WithValue(t.Context(), cleanupSessionContextKey("trace"), "trace-1")
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

	if store == nil {
		t.Fatal("newCleanupSessionStore() returned nil")
	}

	if err := store.Delete(t.Context(), "session-1"); !errors.Is(err, wantErr) {
		t.Fatalf("Delete() error = %v, want %v", err, wantErr)
	}
}

func TestCleanupSessionStoreRevokeFamilyDetachesCancellationAndPreservesError(t *testing.T) {
	want := errors.New("family revocation unavailable")
	store := newCleanupSessionStore(&fakeSessions{revokeFn: func(ctx context.Context, familyID string) error {
		if ctx.Err() != nil || familyID != "family-1" {
			t.Fatalf("cleanup family=%s context=%v", familyID, ctx.Err())
		}

		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > cleanupctx.DefaultTimeout {
			t.Fatal("family revocation must have a bounded cleanup deadline")
		}

		return want
	}})

	if store == nil {
		t.Fatal("newCleanupSessionStore() returned nil")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := store.RevokeFamily(ctx, "family-1"); !errors.Is(err, want) {
		t.Fatalf("RevokeFamily() = %v, want %v", err, want)
	}
}

func TestCleanupSessionStoreExposesFamilyActive(t *testing.T) {
	wantErr := errors.New("family status unavailable")
	store := newCleanupSessionStore(&fakeSessions{familyActiveFn: func(ctx context.Context, familyID string) (bool, error) {
		if ctx != t.Context() || familyID != "family-1" {
			t.Fatalf("FamilyActive() context=%v family=%q", ctx, familyID)
		}

		return false, wantErr
	}})

	if store == nil {
		t.Fatal("newCleanupSessionStore() returned nil")
	}

	active, err := store.FamilyActive(t.Context(), "family-1")
	require.False(t, active)
	require.ErrorIs(t, err, wantErr)
}
