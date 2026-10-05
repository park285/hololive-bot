package member

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCacheAllMembers_SharedLoadHasOwnedDeadline(t *testing.T) {
	var loaderDeadline time.Time

	c := &Cache{
		logger: slog.New(slog.DiscardHandler),
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
	// 호출자 deadline이 길어도 공유 적재는 자기 상한을 쓴다.
	ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
	defer cancel()

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
		logger: slog.New(slog.DiscardHandler),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-releaseFirst

				return []*domain.Member{{ChannelID: "old-channel", Name: testMemberNameOld}}, nil
			}

			return []*domain.Member{{ChannelID: "new-channel", Name: testMemberNameNew}}, nil
		},
	}

	firstDone := make(chan error, 1)

	go func() {
		_, err := c.AllMembers(t.Context())
		firstDone <- err
	}()

	<-firstStarted

	if err := c.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	second, err := c.AllMembers(t.Context())
	if err != nil {
		t.Fatalf("post-invalidate AllMembers() error = %v", err)
	}

	if len(second) != 1 || second[0].Name != testMemberNameNew {
		t.Fatalf("post-invalidate members = %+v, want New generation", second)
	}

	close(releaseFirst)

	if err := <-firstDone; err != nil {
		t.Fatalf("pre-invalidate AllMembers() error = %v", err)
	}

	if calls.Load() != 2 {
		t.Fatalf("loader calls = %d, want 2 generation-specific loads", calls.Load())
	}

	if got, _ := c.lookupPointInMemory(pointLookupName, testMemberNameOld); got != nil {
		t.Fatal("obsolete generation resurrected old name key")
	}
}

// 새 snapshot 게시는 generation을 올려 이전 generation의 point overlay를 통째로 무효화하고, 새 snapshot 색인만 응답한다.
func TestCacheAllMembers_PublishSupersedesPointOverlay(t *testing.T) {
	c := &Cache{logger: slog.New(slog.DiscardHandler)}
	_, generation := c.allMembersView()

	pointOnly := &domain.Member{ID: 7, ChannelID: "point-channel", Name: "PointOnly"}
	c.cacheMember(pointOnly, generation, true)
	c.cacheChannelIDs([]string{"point-channel"}, generation)

	if got, _ := c.lookupPointInMemory(pointLookupChannel, "point-channel"); got != pointOnly {
		t.Fatalf("overlay channel = %+v, want point result before publication", got)
	}

	fresh := &domain.Member{ID: 1, ChannelID: "new-channel", Name: testMemberNameNew}
	if c.storeAllMembersSnapshot(nil, generation, []*domain.Member{fresh}) == nil {
		t.Fatal("snapshot was not published")
	}

	if got, _ := c.lookupPointInMemory(pointLookupChannel, "point-channel"); got != nil {
		t.Fatalf("overlay channel survived publication: %+v", got)
	}

	if got, _ := c.lookupPointInMemory(pointLookupName, "PointOnly"); got != nil {
		t.Fatalf("overlay name survived publication: %+v", got)
	}

	channelIDs, _, ok := c.channelIDsInMemory()
	if !ok || len(channelIDs) != 1 || channelIDs[0] != "new-channel" {
		t.Fatalf("channel IDs = %v, %v; want new snapshot list", channelIDs, ok)
	}

	if got, _ := c.lookupPointInMemory(pointLookupName, testMemberNameNew); got != fresh {
		t.Fatalf("snapshot name = %+v, want %+v", got, fresh)
	}
}

func TestCacheAllMembers_ColdFailureBackoffReturnsSameError(t *testing.T) {
	wantErr := errors.New("cold database outage")

	var calls atomic.Int64

	c := &Cache{
		logger: slog.New(slog.DiscardHandler),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)

			return nil, wantErr
		},
	}

	firstMembers, firstErr := c.AllMembers(t.Context())
	secondMembers, secondErr := c.AllMembers(t.Context())

	if firstMembers != nil || secondMembers != nil {
		t.Fatalf("cold members = (%v, %v), want nil while no snapshot is available", firstMembers, secondMembers)
	}

	if firstErr == nil || !errors.Is(secondErr, firstErr) {
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
	var (
		calls    atomic.Int64
		mu       sync.Mutex
		deadline time.Time
	)

	c := &Cache{
		logger: slog.New(slog.DiscardHandler),
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

	if err := c.WarmUpCache(t.Context()); err != nil {
		t.Fatalf("WarmUpCache() error = %v", err)
	}

	if _, err := c.AllMembers(t.Context()); err != nil {
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

// PostgreSQL point 조회는 잠금 밖에서 끝나므로, 그 사이 snapshot이 교체되면 이전 generation 결과를 메모리에 넣지 않는다.
func TestCachePointLookup_PriorGenerationResultIsNotCached(t *testing.T) {
	c := withTestEpochAuthority(&Cache{logger: slog.New(slog.DiscardHandler)})

	_, lookupGeneration := c.allMembersView()
	if c.storeAllMembersSnapshot(nil, lookupGeneration, []*domain.Member{{ID: 2, ChannelID: "fresh-channel", Name: "Fresh"}}) == nil {
		t.Fatal("snapshot refresh was not published")
	}

	c.cacheMember(&domain.Member{ID: 1, ChannelID: "stale-channel", Name: "Stale"}, lookupGeneration, true)
	c.cacheChannelIDs([]string{"stale-channel"}, lookupGeneration)

	if got, _ := c.lookupPointInMemory(pointLookupName, "Stale"); got != nil {
		t.Fatal("point lookup from the prior generation republished a stale name")
	}

	if got, _ := c.lookupPointInMemory(pointLookupChannel, "stale-channel"); got != nil {
		t.Fatal("point lookup from the prior generation republished a stale channel")
	}

	if channelIDs, _, _ := c.channelIDsInMemory(); len(channelIDs) != 1 || channelIDs[0] != "fresh-channel" {
		t.Fatalf("channel IDs = %v, want prior-generation list rejected", channelIDs)
	}
}

func TestCacheAllMembers_StaleFallbackDefersRepeatedReloads(t *testing.T) {
	stale := testMembers()

	var calls atomic.Int64

	c := &Cache{
		logger:      slog.New(slog.DiscardHandler),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)

			return nil, errors.New("db outage")
		},
	}
	c.allMembersSnapshot.Store(newAllMembersState(stale, 0, time.Now().Add(-2*time.Minute)))

	for attempt := range 2 {
		got, err := c.AllMembers(t.Context())
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
		logger:      slog.New(slog.DiscardHandler),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)

			return testMembers(), nil
		},
	}
	expired := newAllMembersState(testMembers(), 0, time.Now().Add(-2*time.Minute))

	expired.retryAfter = time.Now().Add(-time.Second)
	c.allMembersSnapshot.Store(expired)

	got, err := c.AllMembers(t.Context())
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

// singleflight.DoChan은 공유 적재 panic을 새 goroutine에서 다시 던져 프로세스를 끝낸다. 공유 적재 panic은 모든 대기자에게
// 명시적 실패로 돌아오고, 만료된 성공 snapshot으로 가려지지 않으며, snapshot 상태를 바꾸지 않아 다음 적재가 복구한다.
func TestCacheAllMembers_SharedLoadPanicIsExplicitFailure(t *testing.T) {
	var (
		healthy     atomic.Bool
		panics      atomic.Int64
		startedOnce sync.Once
	)

	started := make(chan struct{})
	release := make(chan struct{})
	stale := newAllMembersState(testMembers(), 0, time.Now().Add(-2*time.Minute))

	c := &Cache{
		logger:      slog.New(slog.DiscardHandler),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			if healthy.Load() {
				return []*domain.Member{{ID: 9, Name: testMemberNameNew}}, nil
			}

			// 첫 적재는 대기자가 합류할 때까지 붙잡고, 이후 적재도 모두 같은 결함으로 panic한다.
			startedOnce.Do(func() { close(started) })
			<-release
			panics.Add(1)

			panic("nil PostgreSQL pool")
		},
	}
	c.allMembersSnapshot.Store(stale)

	const waiters = 4

	errs := make(chan error, waiters)

	var wg sync.WaitGroup

	wg.Go(func() {
		_, err := c.AllMembers(t.Context())
		errs <- err
	})

	<-started

	for range waiters - 1 {
		wg.Go(func() {
			_, err := c.MembersByName(t.Context(), testMemberNameOld)
			errs <- err
		})
	}

	close(release)
	wg.Wait()
	close(errs)

	if panics.Load() == 0 {
		t.Fatal("loader did not panic")
	}

	for err := range errs {
		if !errors.Is(err, errAllMembersLoadPanicked) {
			t.Fatalf("waiter error = %v, want explicit panic failure instead of stale success", err)
		}
	}

	if c.allMembersSnapshot.Load() != stale {
		t.Fatal("panicked load changed the published snapshot state")
	}

	healthy.Store(true)

	got, err := c.AllMembers(t.Context())
	if err != nil || len(got) != 1 || got[0].Name != testMemberNameNew {
		t.Fatalf("AllMembers() after panic = %+v, %v; want recovered reload", got, err)
	}
}

// PostgreSQL pool 없는 repository는 첫 조회 panic 대신 구성 오류로 거절한다.
func TestNewMemberCacheRejectsRepositoryWithoutPool(t *testing.T) {
	cache, err := NewMemberCache(t.Context(), &Repository{}, nil, slog.New(slog.DiscardHandler), CacheConfig{WarmUp: true})
	if err == nil || cache != nil {
		t.Fatalf("NewMemberCache() = %v, %v; want configuration error", cache, err)
	}
}
