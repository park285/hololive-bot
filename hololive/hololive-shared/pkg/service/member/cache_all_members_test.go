// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

const (
	testMemberNameOld = "Old"
	testMemberNameNew = "New"
	// TestMemberMiko·testMemberMikoSlug·testMemberPekora는 패키지 테스트가 함께 쓰는 멤버 이름 fixture다.
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
		got, err := c.AllMembers(t.Context())
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

	if _, err := c.AllMembers(t.Context()); err != nil {
		t.Fatalf("first AllMembers() error = %v", err)
	}

	if err := c.InvalidateAll(t.Context()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}

	if _, err := c.AllMembers(t.Context()); err != nil {
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

	if _, err := c.AllMembers(t.Context()); err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Fatalf("loader called %d times, want 1 (expired snapshot must reload)", n)
	}

	if _, err := c.AllMembers(t.Context()); err != nil {
		t.Fatalf("second AllMembers() error = %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Fatalf("loader called %d times, want 1 (fresh snapshot must be reused)", n)
	}
}

func TestCacheAllMembers_ConcurrentCallsConvergeToSingleLoad(t *testing.T) {
	t.Parallel()

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

			results[i], errs[i] = c.AllMembers(t.Context())
		})
	}

	close(start)
	time.Sleep(50 * time.Millisecond)
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

	cached, err := c.AllMembers(t.Context())
	if err != nil || cached[0] != firstMember {
		t.Fatalf("caller mutated cached snapshot: members=%v error=%v", cached, err)
	}
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

	got, err := c.AllMembers(t.Context())
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

// doneObservedContext는 Done을 처음 부를 때 알린다. AllMembers는 공유 적재 대기 select에서만 Done을 부르므로, 대기자가
// 진행 중인 공유 적재에 합류했다는 시점을 sleep 없이 관측한다.
type doneObservedContext struct {
	context.Context //nolint:containedctx // 공유 적재 합류 시점을 관찰하는 Context wrapper이며 원래 취소·값 계약을 그대로 위임한다.

	observed chan struct{}
	once     sync.Once
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })

	return c.Context.Done()
}

// blockingSnapshotLoader는 release 전까지 공유 적재를 붙잡고, 적재 ctx가 취소된 채 끝났는지 기록한다.
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

// startAllMembers는 AllMembers를 별도 goroutine에서 실행하고 결과 오류를 돌려준다. 성공했는데 snapshot이 불완전하면 오류다.
func startAllMembers(ctx context.Context, c *Cache) <-chan error {
	done := make(chan error, 1)

	go func() {
		members, err := c.AllMembers(ctx)
		if err == nil && len(members) != len(testMembers()) {
			err = errors.New("waiter received an incomplete snapshot")
		}

		done <- err
	}()

	return done
}

// 진행 중인 공유 적재에 합류한 대기자가 취소되면 자기만 즉시 빠지고, 공유 적재는 호출자 취소와 분리된 채 끝나 다른
// 대기자와 snapshot을 채운다.
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

	if _, err := c.AllMembers(t.Context()); err != nil || loader.calls.Load() != 1 {
		t.Fatalf("snapshot after canceled waiter = err %v calls %d, want published snapshot reuse", err, loader.calls.Load())
	}
}

// 이미 취소된 호출자는 공유 적재를 시작하지 않고 ctx 오류를 받는다.
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

	if _, err := c.AllMembers(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("AllMembers() error = %v, want context.Canceled", err)
	}

	if calls.Load() != 0 {
		t.Fatalf("loader calls = %d, want 0 for an already canceled caller", calls.Load())
	}
}

func TestCacheAllMembers_NilRepositoryReturnsError(t *testing.T) {
	t.Parallel()

	c := &Cache{logger: slog.New(slog.DiscardHandler)}

	_, err := c.AllMembers(t.Context())
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

	first, err := c.AllMembers(t.Context())
	if err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}

	first[0] = nil

	second, err := c.AllMembers(t.Context())
	if err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}

	if second[0] == nil {
		t.Fatal("AllMembers() must return an independent slice; caller mutation leaked into snapshot")
	}
}

func TestCacheInvalidateAllRacesWithSnapshotReload(t *testing.T) {
	t.Parallel()

	c := &Cache{
		logger:      slog.New(slog.DiscardHandler),
		snapshotTTL: time.Nanosecond,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			return testMembers(), nil
		},
	}

	var wg sync.WaitGroup

	stop := make(chan struct{})

	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}

			if _, err := c.AllMembers(t.Context()); err != nil {
				t.Errorf("AllMembers() error = %v", err)

				return
			}
		}
	})

	wg.Go(func() {
		for range 200 {
			if err := c.InvalidateAll(t.Context()); err != nil {
				t.Errorf("InvalidateAll() error = %v", err)

				return
			}
		}
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(stop)
	}()

	wg.Wait()
}
