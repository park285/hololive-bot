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
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func testMembers() []*domain.Member {
	return []*domain.Member{
		{ChannelID: "UC_pekora", Name: "Pekora"},
		{ChannelID: "UC_miko", Name: "Miko"},
		{Name: "NameOnly"},
	}
}

func TestCacheAllMembers_ReusesSnapshotAcrossCalls(t *testing.T) {
	t.Parallel()

	var calls int64
	c := &Cache{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			atomic.AddInt64(&calls, 1)
			return testMembers(), nil
		},
	}

	for i := range 5 {
		got, err := c.AllMembers(context.Background())
		if err != nil {
			t.Fatalf("AllMembers() call %d error = %v", i, err)
		}
		if len(got) != 3 {
			t.Fatalf("AllMembers() call %d len = %d, want 3", i, len(got))
		}
	}

	if n := atomic.LoadInt64(&calls); n != 1 {
		t.Fatalf("loader called %d times, want 1 (steady-state must not reload)", n)
	}

	if _, ok := c.byChannelID.Load("UC_pekora"); !ok {
		t.Fatal("snapshot load must backfill byChannelID map")
	}
	if _, ok := c.byName.Load("Miko"); !ok {
		t.Fatal("snapshot load must backfill byName map")
	}
}

func TestCacheAllMembers_ReloadsAfterInvalidateAll(t *testing.T) {
	t.Parallel()

	var calls int64
	c := &Cache{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			atomic.AddInt64(&calls, 1)
			return testMembers(), nil
		},
	}

	if _, err := c.AllMembers(context.Background()); err != nil {
		t.Fatalf("first AllMembers() error = %v", err)
	}
	if err := c.InvalidateAll(context.Background()); err != nil {
		t.Fatalf("InvalidateAll() error = %v", err)
	}
	if _, err := c.AllMembers(context.Background()); err != nil {
		t.Fatalf("second AllMembers() error = %v", err)
	}

	if n := atomic.LoadInt64(&calls); n != 2 {
		t.Fatalf("loader called %d times, want 2 (invalidation must force one reload)", n)
	}
}

func TestCacheAllMembers_ReloadsAfterTTLExpiry(t *testing.T) {
	t.Parallel()

	var calls int64
	c := &Cache{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			atomic.AddInt64(&calls, 1)
			return testMembers(), nil
		},
	}

	c.allMembersSnapshot.Store(&allMembersState{
		members:  testMembers(),
		loadedAt: time.Now().Add(-2 * time.Minute),
	})

	if _, err := c.AllMembers(context.Background()); err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}
	if n := atomic.LoadInt64(&calls); n != 1 {
		t.Fatalf("loader called %d times, want 1 (expired snapshot must reload)", n)
	}

	if _, err := c.AllMembers(context.Background()); err != nil {
		t.Fatalf("second AllMembers() error = %v", err)
	}
	if n := atomic.LoadInt64(&calls); n != 1 {
		t.Fatalf("loader called %d times, want 1 (fresh snapshot must be reused)", n)
	}
}

func TestCacheAllMembers_ConcurrentCallsConvergeToSingleLoad(t *testing.T) {
	t.Parallel()

	var calls int64
	release := make(chan struct{})
	c := &Cache{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			atomic.AddInt64(&calls, 1)
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
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = c.AllMembers(context.Background())
		}(i)
	}

	close(start)
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if n := atomic.LoadInt64(&calls); n != 1 {
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
}

func TestCacheAllMembers_ExpiredSnapshotFallsBackOnLoaderFailure(t *testing.T) {
	t.Parallel()

	stale := testMembers()
	var calls int64
	c := &Cache{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		snapshotTTL: time.Minute,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			atomic.AddInt64(&calls, 1)
			return nil, fmt.Errorf("db outage")
		},
	}
	c.allMembersSnapshot.Store(&allMembersState{
		members:  stale,
		loadedAt: time.Now().Add(-2 * time.Minute),
	})

	got, err := c.AllMembers(context.Background())
	if err != nil {
		t.Fatalf("AllMembers() error = %v, want nil (must serve stale snapshot on reload failure)", err)
	}
	if len(got) != len(stale) {
		t.Fatalf("AllMembers() len = %d, want %d (expired snapshot must be returned, not empty)", len(got), len(stale))
	}
	if n := atomic.LoadInt64(&calls); n != 1 {
		t.Fatalf("loader called %d times, want 1 (reload attempted once before fallback)", n)
	}
	if snap := c.allMembersSnapshot.Load(); snap == nil {
		t.Fatal("stale snapshot must be retained after failed reload, not cleared")
	}
}

func TestCacheAllMembers_LoadSurvivesCallerCancellation(t *testing.T) {
	t.Parallel()

	var loaderCtxErr error
	c := &Cache{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(ctx context.Context) ([]*domain.Member, error) {
			loaderCtxErr = ctx.Err()
			return testMembers(), nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := c.AllMembers(ctx)
	if err != nil {
		t.Fatalf("AllMembers() error = %v, want nil despite cancelled caller ctx", err)
	}
	if len(got) != 3 {
		t.Fatalf("AllMembers() len = %d, want 3", len(got))
	}
	if loaderCtxErr != nil {
		t.Fatalf("loader received ctx err = %v, want nil (caller cancellation must not enter the shared load)", loaderCtxErr)
	}
}

func TestCacheAllMembers_NilRepositoryReturnsError(t *testing.T) {
	t.Parallel()

	c := &Cache{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	_, err := c.AllMembers(context.Background())
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
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			return testMembers(), nil
		},
	}

	first, err := c.AllMembers(context.Background())
	if err != nil {
		t.Fatalf("AllMembers() error = %v", err)
	}
	first[0] = nil

	second, err := c.AllMembers(context.Background())
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
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		snapshotTTL: time.Nanosecond,
		loadAllMembers: func(context.Context) ([]*domain.Member, error) {
			return testMembers(), nil
		},
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := c.AllMembers(context.Background()); err != nil {
				t.Errorf("AllMembers() error = %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			if err := c.InvalidateAll(context.Background()); err != nil {
				t.Errorf("InvalidateAll() error = %v", err)
				return
			}
		}
	}()

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(stop)
	}()
	wg.Wait()
}
