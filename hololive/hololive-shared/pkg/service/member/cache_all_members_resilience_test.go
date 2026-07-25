package member

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCacheAllMembers_SharedLoadHasOwnedDeadline(t *testing.T) {
	var loaderDeadline time.Time
	c := &Cache{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(ctx context.Context) ([]*domain.Member, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("shared member snapshot load has no deadline")
			}
			if ctx.Err() != nil {
				t.Fatalf("shared member snapshot load inherited caller cancellation: %v", ctx.Err())
			}
			loaderDeadline = deadline
			return testMembers(), nil
		},
	}

	startedAt := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.AllMembers(ctx); err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}

	deadlineBudget := loaderDeadline.Sub(startedAt)
	if deadlineBudget < allMembersSnapshotLoadTimeout-time.Second || deadlineBudget > allMembersSnapshotLoadTimeout+time.Second {
		t.Fatalf("shared load deadline budget = %v, want near %v", deadlineBudget, allMembersSnapshotLoadTimeout)
	}
}

func TestCacheAllMembers_StaleFallbackDefersRepeatedReloads(t *testing.T) {
	stale := testMembers()
	var calls atomic.Int64
	c := &Cache{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)
			return nil, errors.New("db outage")
		},
	}
	c.allMembersSnapshot.Store(&allMembersState{
		members:  stale,
		loadedAt: time.Now().Add(-2 * time.Minute),
	})

	for attempt := 0; attempt < 2; attempt++ {
		got, err := c.AllMembers(context.Background())
		if err != nil {
			t.Fatalf("AllMembers() attempt %d error = %v", attempt+1, err)
		}
		if len(got) != len(stale) {
			t.Fatalf("AllMembers() attempt %d len = %d, want %d", attempt+1, len(got), len(stale))
		}
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1 while retry backoff is active", got)
	}
	snap := c.allMembersSnapshot.Load()
	if snap == nil || !snap.retryAfter.After(time.Now()) {
		t.Fatalf("retry_after = %v, want future retry boundary", snap)
	}
}

func TestCacheAllMembers_ReloadsAfterRetryBoundaryAndClearsBackoff(t *testing.T) {
	var calls atomic.Int64
	c := &Cache{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)
			return testMembers(), nil
		},
	}
	c.allMembersSnapshot.Store(&allMembersState{
		members:    testMembers(),
		loadedAt:   time.Now().Add(-2 * time.Minute),
		retryAfter: time.Now().Add(-time.Second),
	})

	got, err := c.AllMembers(context.Background())
	if err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}
	if len(got) != len(testMembers()) {
		t.Fatalf("AllMembers() len = %d, want %d", len(got), len(testMembers()))
	}
	if calls.Load() != 1 {
		t.Fatalf("loader calls = %d, want 1 after retry boundary", calls.Load())
	}
	if snap := c.allMembersSnapshot.Load(); snap == nil || !snap.retryAfter.IsZero() {
		t.Fatalf("recovered snapshot retry_after = %v, want zero", snap)
	}
}
