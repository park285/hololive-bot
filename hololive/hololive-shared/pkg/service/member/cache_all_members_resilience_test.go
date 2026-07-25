package member

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCacheAllMembers_SharedLoadStopsAtOwnedDeadline(t *testing.T) {
	c := &Cache{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(ctx context.Context) ([]*domain.Member, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	startedAt := time.Now()
	_, err := c.loadAllMembersSnapshotWithin(context.Background(), nil, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("loadAllMembersSnapshotWithin() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("shared load elapsed = %v, want bounded deadline", elapsed)
	}
}

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

	for attempt := range 2 {
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
	var logs bytes.Buffer
	c := &Cache{
		logger:      slog.New(slog.NewTextHandler(&logs, nil)),
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
	if got := logs.String(); !strings.Contains(got, "member_snapshot_reload_recovered") {
		t.Fatalf("recovery log = %q, want member_snapshot_reload_recovered", got)
	}
}
