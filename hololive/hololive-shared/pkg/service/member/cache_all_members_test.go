package member

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testMemberNameOld  = "Old"
	testMemberNameNew  = "New"
	testMemberMiko     = "Miko"
	testMemberMikoSlug = "miko"
	testMemberPekora   = "Pekora"
)

func testMembers() []*domain.Member {
	return []*domain.Member{
		{ChannelID: "UC_pekora", Name: testMemberPekora},
		{ChannelID: "UC_miko", Name: testMemberMiko},
		{Name: "NameOnly"},
	}
}

func TestCacheAllMembers_ReusesSnapshotAcrossCalls(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	c := &Cache{
		logger: slog.New(slog.DiscardHandler),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)

			return testMembers(), nil
		},
	}

	for i := range 5 {
		got, err := c.LoadAllMembers(t.Context())
		if err != nil {
			t.Fatalf("AllMembers() call %d error = %v", i, err)
		}

		if len(got) != 3 {
			t.Fatalf("AllMembers() call %d len = %d, want 3", i, len(got))
		}
	}

	if n := calls.Load(); n != 1 {
		t.Fatalf("loader called %d times, want 1 (steady-state must not reload)", n)
	}

	if got, _ := c.lookupPointInMemory(pointLookupChannel, "UC_pekora"); got == nil {
		t.Fatal("snapshot load must serve channel lookups from memory")
	}

	if got, _ := c.lookupPointInMemory(pointLookupName, testMemberMiko); got == nil {
		t.Fatal("snapshot load must serve name lookups from memory")
	}
}

func TestCacheAllMembers_ReloadsAfterInvalidateAll(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	c := &Cache{
		logger: slog.New(slog.DiscardHandler),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)

			return testMembers(), nil
		},
	}

	if _, err := c.LoadAllMembers(t.Context()); err != nil {
		t.Fatalf("first AllMembers() error = %v", err)
	}

	if err := c.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	if _, err := c.LoadAllMembers(t.Context()); err != nil {
		t.Fatalf("second AllMembers() error = %v", err)
	}

	if n := calls.Load(); n != 2 {
		t.Fatalf("loader called %d times, want 2 (invalidation must force one reload)", n)
	}
}

func TestCacheAllMembers_ReloadsAfterTTLExpiry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	c := &Cache{
		logger:      slog.New(slog.DiscardHandler),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)

			return testMembers(), nil
		},
	}

	c.allMembersSnapshot.Store(newAllMembersState(testMembers(), 0, time.Now().Add(-2*time.Minute)))

	if _, err := c.LoadAllMembers(t.Context()); err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Fatalf("loader called %d times, want 1 (expired snapshot must reload)", n)
	}

	if _, err := c.LoadAllMembers(t.Context()); err != nil {
		t.Fatalf("second AllMembers() error = %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Fatalf("loader called %d times, want 1 (fresh snapshot must be reused)", n)
	}
}

func TestCacheAllMembers_ConcurrentCallsConvergeToSingleLoad(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64

		release := make(chan struct{})
		c := &Cache{
			logger: slog.New(slog.DiscardHandler),
			loadAllMembers: func(context.Context) ([]*domain.Member, error) {
				calls.Add(1)
				<-release

				return testMembers(), nil
			},
		}

		const goroutines = 50

		start := make(chan struct{})

		var wg sync.WaitGroup

		results := make([][]*domain.Member, goroutines)
		errs := make([]error, goroutines)

		for i := range goroutines {
			wg.Go(func() {
				<-start

				results[i], errs[i] = c.LoadAllMembers(t.Context())
			})
		}

		close(start)
		synctest.Wait()
		close(release)
		wg.Wait()

		if n := calls.Load(); n != 1 {
			t.Fatalf("loader called %d times under concurrent stampede, want 1", n)
		}

		for i := range goroutines {
			if errs[i] != nil {
				t.Fatalf("goroutine %d error = %v", i, errs[i])
			}

			if len(results[i]) != 3 {
				t.Fatalf("goroutine %d len = %d, want 3", i, len(results[i]))
			}
		}

		firstMember := results[0][0]

		results[0][0] = nil

		for i := 1; i < goroutines; i++ {
			if results[i][0] != firstMember {
				t.Fatalf("caller 0 mutated caller %d's result slice", i)
			}
		}

		cached, err := c.LoadAllMembers(t.Context())
		if err != nil || cached[0] != firstMember {
			t.Fatalf("caller mutated cached snapshot: members=%v error=%v", cached, err)
		}
	})
}

func TestCacheAllMembers_ExpiredSnapshotFallsBackOnLoaderFailure(t *testing.T) {
	t.Parallel()

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

	got, err := c.LoadAllMembers(t.Context())
	if err != nil {
		t.Fatalf("AllMembers() error = %v, want nil (must serve stale snapshot on reload failure)", err)
	}

	if len(got) != len(stale) {
		t.Fatalf("AllMembers() len = %d, want %d (expired snapshot must be returned, not empty)", len(got), len(stale))
	}

	if n := calls.Load(); n != 1 {
		t.Fatalf("loader called %d times, want 1 (reload attempted once before fallback)", n)
	}

	if snap := c.allMembersSnapshot.Load(); snap == nil {
		t.Fatal("stale snapshot must be retained after failed reload, not cleared")
	}
}

type doneObservedContext struct {
	context.Context //nolint:containedctx // 공유 적재 합류 시점을 관찰하는 Context wrapper이며 원래 취소·값 계약을 그대로 위임한다.

	observed chan struct{}
	once     sync.Once
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })

	return c.Context.Done()
}

type blockingSnapshotLoader struct {
	started       chan struct{}
	release       chan struct{}
	calls         atomic.Int64
	canceledLoads atomic.Int64
}

func newBlockingSnapshotLoader() *blockingSnapshotLoader {
	return &blockingSnapshotLoader{started: make(chan struct{}), release: make(chan struct{})}
}

func (l *blockingSnapshotLoader) load(ctx context.Context) ([]*domain.Member, error) {
	if l.calls.Add(1) == 1 {
		close(l.started)
	}

	<-l.release

	if ctx.Err() != nil {
		l.canceledLoads.Add(1)
	}

	return testMembers(), nil
}

func startAllMembers(ctx context.Context, c *Cache) <-chan error {
	done := make(chan error, 1)

	go func() {
		members, err := c.LoadAllMembers(ctx)
		if err == nil && len(members) != len(testMembers()) {
			err = errors.New("waiter received an incomplete snapshot")
		}

		done <- err
	}()

	return done
}

func TestCacheAllMembers_CanceledWaiterLeavesSharedLoadRunning(t *testing.T) {
	t.Parallel()

	loader := newBlockingSnapshotLoader()
	c := &Cache{logger: slog.New(slog.DiscardHandler), loadAllMembers: loader.load}

	ownerDone := startAllMembers(t.Context(), c)

	<-loader.started

	waiterCtx, cancelWaiter := context.WithCancel(t.Context())
	observed := &doneObservedContext{Context: waiterCtx, observed: make(chan struct{})}
	waiterDone := startAllMembers(observed, c)

	<-observed.observed
	cancelWaiter()

	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter stayed blocked on the shared load")
	}

	close(loader.release)

	if err := <-ownerDone; err != nil {
		t.Fatalf("load owner error = %v, want shared snapshot", err)
	}

	if loader.canceledLoads.Load() != 0 || loader.calls.Load() != 1 {
		t.Fatalf("shared load canceled=%d calls=%d, want one uncanceled load", loader.canceledLoads.Load(), loader.calls.Load())
	}

	if _, err := c.LoadAllMembers(t.Context()); err != nil || loader.calls.Load() != 1 {
		t.Fatalf("snapshot after canceled waiter = err %v calls %d, want published snapshot reuse", err, loader.calls.Load())
	}
}

func TestCacheAllMembers_PreCanceledCallerDoesNotStartLoad(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	c := &Cache{
		logger: slog.New(slog.DiscardHandler),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			calls.Add(1)

			return testMembers(), nil
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := c.LoadAllMembers(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("AllMembers() error = %v, want context.Canceled", err)
	}

	if calls.Load() != 0 {
		t.Fatalf("loader calls = %d, want 0 for an already canceled caller", calls.Load())
	}
}

func TestCacheAllMembers_NilRepositoryReturnsError(t *testing.T) {
	t.Parallel()

	c := &Cache{logger: slog.New(slog.DiscardHandler)}

	_, err := c.LoadAllMembers(t.Context())
	if err == nil {
		t.Fatal("AllMembers() error = nil, want non-nil")
	}

	if got := err.Error(); got != "member repository is nil" {
		t.Fatalf("AllMembers() error = %q, want %q", got, "member repository is nil")
	}
}

func TestCacheAllMembers_ReturnsClonedSlice(t *testing.T) {
	t.Parallel()

	c := &Cache{
		logger: slog.New(slog.DiscardHandler),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			return testMembers(), nil
		},
	}

	first, err := c.LoadAllMembers(t.Context())
	if err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}

	first[0] = nil

	second, err := c.LoadAllMembers(t.Context())
	if err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}

	if second[0] == nil {
		t.Fatal("AllMembers() must return an independent slice; caller mutation leaked into snapshot")
	}
}

func TestCacheInvalidateAllRacesWithSnapshotReload(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const invalidations = 200

		started := make(chan struct{})
		resume := make(chan struct{})

		var loads atomic.Int32

		c := &Cache{
			logger: slog.New(slog.DiscardHandler),
			loadAllMembers: func(context.Context) ([]*domain.Member, error) {
				if loads.Add(1) <= invalidations {
					started <- struct{}{}

					<-resume

					return []*domain.Member{{Name: testMemberNameOld}}, nil
				}

				return []*domain.Member{{Name: testMemberNameNew}}, nil
			},
		}

		done := make(chan struct{})

		var (
			members []*domain.Member
			loadErr error
		)

		go func() {
			defer close(done)

			members, loadErr = c.LoadAllMembers(t.Context())
		}()

		for round := range invalidations {
			select {
			case <-started:
			case <-done:
				t.Fatalf("reload finished during invalidation %d: members=%v, err=%v", round, members, loadErr)
			}

			if err := c.InvalidateAll(t.Context()); err != nil {
				t.Fatal(err)
			}

			resume <- struct{}{}
		}

		<-done

		if loadErr != nil || loads.Load() != invalidations+1 || len(members) != 1 || members[0].Name != testMemberNameNew {
			t.Fatalf("reload after %d invalidations: loads=%d members=%v err=%v", invalidations, loads.Load(), members, loadErr)
		}
	})
}
