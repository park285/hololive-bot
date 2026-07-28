package member

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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

func TestCacheAllMembers_InvalidateSeparatesInFlightGeneration(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int64
	c := &Cache{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-releaseFirst
				return []*domain.Member{{ChannelID: "old-channel", Name: "Old"}}, nil
			}
			return []*domain.Member{{ChannelID: "new-channel", Name: "New"}}, nil
		},
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := c.AllMembers(context.Background())
		firstDone <- err
	}()
	<-firstStarted

	if err := c.InvalidateAll(context.Background()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}
	second, err := c.AllMembers(context.Background())
	if err != nil {
		t.Fatalf("post-invalidate AllMembers() error = %v", err)
	}
	if len(second) != 1 || second[0].Name != "New" {
		t.Fatalf("post-invalidate members = %+v, want New generation", second)
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("pre-invalidate AllMembers() error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("loader calls = %d, want 2 generation-specific loads", calls.Load())
	}
	if _, ok := c.byName.Load("Old"); ok {
		t.Fatal("obsolete generation resurrected old name key")
	}
}

func TestCacheAllMembers_PublishRemovesOnlyOwnedStaleKeys(t *testing.T) {
	old := &domain.Member{ChannelID: "old-channel", Name: "Old"}
	removed := &domain.Member{ChannelID: "removed-channel", Name: "Removed"}
	newerOwner := &domain.Member{ChannelID: "old-channel", Name: "Independent"}
	c := &Cache{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), snapshotTTL: time.Nanosecond}
	c.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return []*domain.Member{old, removed}, nil
	}
	if _, err := c.AllMembers(context.Background()); err != nil {
		t.Fatalf("initial AllMembers() error = %v", err)
	}
	c.byChannelID.Store(old.ChannelID, newerOwner)
	c.loadAllMembers = func(context.Context) ([]*domain.Member, error) {
		return []*domain.Member{{ChannelID: "new-channel", Name: "New"}}, nil
	}
	c.allMembersSnapshot.Load().loadedAt = time.Now().Add(-time.Second)
	if _, err := c.AllMembers(context.Background()); err != nil {
		t.Fatalf("reload AllMembers() error = %v", err)
	}

	if _, ok := c.byName.Load(old.Name); ok {
		t.Fatal("renamed member left its old name key behind")
	}
	if got, ok := c.byChannelID.Load(old.ChannelID); !ok || got != newerOwner {
		t.Fatalf("independently owned channel key = %v, %v; want preserved", got, ok)
	}
	if _, ok := c.byChannelID.Load(removed.ChannelID); ok {
		t.Fatal("removed member left its old channel key behind")
	}
	if _, ok := c.byName.Load("New"); !ok {
		t.Fatal("new snapshot name key was not published")
	}
}

func TestCacheAllMembers_ColdFailureBackoffReturnsSameError(t *testing.T) {
	wantErr := errors.New("cold database outage")
	var calls atomic.Int64
	c := &Cache{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)
			return nil, wantErr
		},
	}

	_, firstErr := c.AllMembers(context.Background())
	_, secondErr := c.AllMembers(context.Background())
	if firstErr == nil || secondErr != firstErr {
		t.Fatalf("cold errors = (%v, %v), want same cached error", firstErr, secondErr)
	}
	if !errors.Is(firstErr, wantErr) {
		t.Fatalf("cold error = %v, want wrapped %v", firstErr, wantErr)
	}
	if calls.Load() != 1 {
		t.Fatalf("loader calls = %d, want 1 during cold retry backoff", calls.Load())
	}
}

func TestCacheWarmUp_UsesBoundedCanonicalSnapshotOnce(t *testing.T) {
	var calls atomic.Int64
	var mu sync.Mutex
	var deadline time.Time
	c := &Cache{
		logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		warmUpChunkSize:     1,
		warmUpMaxGoroutines: 1,
		loadAllMembers: func(ctx context.Context) ([]*domain.Member, error) {
			calls.Add(1)
			got, ok := ctx.Deadline()
			if !ok {
				t.Fatal("warmup snapshot loader has no deadline")
			}
			mu.Lock()
			deadline = got
			mu.Unlock()
			return testMembers(), nil
		},
	}

	startedAt := time.Now()
	if err := c.WarmUpCache(context.Background()); err != nil {
		t.Fatalf("WarmUpCache() error = %v", err)
	}
	if _, err := c.AllMembers(context.Background()); err != nil {
		t.Fatalf("AllMembers() after warmup error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("loader calls = %d, want one canonical scan", calls.Load())
	}
	mu.Lock()
	deadlineBudget := deadline.Sub(startedAt)
	mu.Unlock()
	if deadlineBudget < allMembersSnapshotLoadTimeout-time.Second || deadlineBudget > allMembersSnapshotLoadTimeout+time.Second {
		t.Fatalf("warmup deadline budget = %v, want near %v", deadlineBudget, allMembersSnapshotLoadTimeout)
	}
}

func TestCachePointLookup_SerializesWithSnapshotRefresh(t *testing.T) {
	lookupStarted := make(chan struct{})
	releaseLookup := make(chan struct{})
	cacheClient := cachemocks.NewLenientClient()
	cacheClient.GetFunc = func(_ context.Context, _ string, dest any) error {
		close(lookupStarted)
		<-releaseLookup
		member := dest.(*domain.Member)
		*member = domain.Member{ChannelID: "stale-channel", Name: "Stale"}
		return nil
	}
	c := &Cache{
		cache:  cacheClient,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	lookupDone := make(chan error, 1)
	go func() {
		_, err := c.GetByName(context.Background(), "Stale")
		lookupDone <- err
	}()
	<-lookupStarted

	_, generation := c.allMembersView()
	refreshDone := make(chan bool, 1)
	go func() {
		refreshDone <- c.storeAllMembersSnapshot(nil, generation, []*domain.Member{{ChannelID: "fresh-channel", Name: "Fresh"}})
	}()
	close(releaseLookup)
	if err := <-lookupDone; err != nil {
		t.Fatalf("GetByName() error = %v", err)
	}
	if !<-refreshDone {
		t.Fatal("snapshot refresh was not published")
	}
	if _, ok := c.byName.Load("Stale"); ok {
		t.Fatal("point lookup from the prior generation republished a stale name key")
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
