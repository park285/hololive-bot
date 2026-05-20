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

package ingestionlease

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newTestCacheForLock(t *testing.T) *cache.Service {
	t.Helper()
	svc, _ := newTestCacheForLockWithMini(t)
	return svc
}

func newTestCacheForLockWithMini(t *testing.T) (*cache.Service, *miniredis.Miniredis) {
	t.Helper()

	mini := miniredis.RunT(t)
	port, err := strconv.Atoi(mini.Port())
	if err != nil {
		t.Fatalf("parse miniredis port: %v", err)
	}
	svc, err := cache.NewCacheService(context.Background(), cache.Config{
		Host:              mini.Host(),
		Port:              port,
		DB:                0,
		DisableCache:      true,
		ForceSingleClient: true,
	}, sharedlogging.NewTestLogger())
	if err != nil {
		t.Fatalf("new cache service: %v", err)
	}
	t.Cleanup(func() {
		_ = svc.Close()
		mini.Close()
	})
	return svc, mini
}

func TestAcquireExclusive(t *testing.T) {
	var held bool
	var owner string
	cacheSvc := &cachemocks.Client{
		SetNXFunc: func(_ context.Context, key, value string, _ time.Duration) (bool, error) {
			if key != Key {
				return false, errors.New("unexpected key")
			}
			if held {
				return false, nil
			}
			held = true
			owner = value
			return true, nil
		},
		CompareAndDeleteFunc: func(_ context.Context, key, expectedValue string) (bool, error) {
			if key != Key {
				return false, errors.New("unexpected key")
			}
			if !held {
				return false, nil
			}
			if expectedValue != owner {
				return false, nil
			}
			held = false
			owner = ""
			return true, nil
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	first, err := Acquire(context.Background(), cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}

	if _, err := Acquire(context.Background(), cacheSvc, "youtube-producer", logger); err == nil {
		t.Fatalf("expected second acquisition to fail")
	}

	if err := first.Release(context.Background()); err != nil {
		t.Fatalf("release first lease: %v", err)
	}

	if _, err := Acquire(context.Background(), cacheSvc, "youtube-producer", logger); err != nil {
		t.Fatalf("lease after release should succeed: %v", err)
	}
}

func TestLeaseRenewLoop(t *testing.T) {
	cacheSvc := newTestCacheForLock(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := Acquire(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	lease.ttl = time.Second
	lease.renewInterval = 200 * time.Millisecond
	if err := cacheSvc.Expire(ctx, Key, lease.ttl); err != nil {
		t.Fatalf("shorten ttl: %v", err)
	}

	renewCtx := t.Context()
	go lease.StartRenewLoop(renewCtx, nil)

	time.Sleep(1300 * time.Millisecond)

	exists, err := cacheSvc.Exists(ctx, Key)
	if err != nil {
		t.Fatalf("exists check: %v", err)
	}
	if !exists {
		t.Fatalf("lease key should still exist due to renew loop")
	}
}

func TestLeaseRenewOwnershipLost(t *testing.T) {
	cacheSvc := newTestCacheForLock(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := Acquire(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	if err := cacheSvc.GetClient().Do(ctx, cacheSvc.B().Set().Key(Key).Value("other-owner").Build()).Error(); err != nil {
		t.Fatalf("override lock owner: %v", err)
	}

	err = lease.renew(ctx)
	if err == nil {
		t.Fatalf("expected ownership lost error")
	}
	if !errors.Is(err, errIngestionLeaseOwnershipLost) {
		t.Fatalf("expected errIngestionLeaseOwnershipLost, got %v", err)
	}
}

func TestLeaseRenewTransientFailure(t *testing.T) {
	cacheSvc, mini := newTestCacheForLockWithMini(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := Acquire(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	lease.retrySleep = func(_ context.Context, _ time.Duration) bool { return true }

	mini.SetError("LOADING Redis is loading the dataset in memory")

	var attempt atomic.Int32
	origSleep := lease.retrySleep
	lease.retrySleep = func(ctx context.Context, d time.Duration) bool {
		if attempt.Add(1) >= 2 {
			mini.SetError("")
		}
		return origSleep(ctx, d)
	}

	if err := lease.renew(ctx); err != nil {
		t.Fatalf("renew should succeed after transient failures: %v", err)
	}
	if attempt.Load() < 1 {
		t.Fatalf("expected at least 1 retry, got %d", attempt.Load())
	}
}

func TestLeaseRenewTransientExhausted(t *testing.T) {
	cacheSvc, mini := newTestCacheForLockWithMini(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := Acquire(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	lease.retrySleep = func(_ context.Context, _ time.Duration) bool { return true }

	mini.SetError("LOADING Redis is loading the dataset in memory")

	err = lease.renew(ctx)
	if err == nil {
		t.Fatalf("renew should fail after exhausting retries")
	}
	if errors.Is(err, errIngestionLeaseOwnershipLost) {
		t.Fatalf("transient error should not be classified as ownership lost")
	}
}

func TestLeaseRenewLoopReportsOwnershipLost(t *testing.T) {
	cacheSvc := newTestCacheForLock(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	lease, err := Acquire(ctx, cacheSvc, "bot", logger)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	lease.renewInterval = 20 * time.Millisecond

	errCh := make(chan error, 1)
	renewCtx := t.Context()
	go lease.StartRenewLoop(renewCtx, errCh)

	if err := cacheSvc.GetClient().Do(ctx, cacheSvc.B().Set().Key(Key).Value("other-owner").Build()).Error(); err != nil {
		t.Fatalf("override lock owner: %v", err)
	}

	select {
	case gotErr := <-errCh:
		if !errors.Is(gotErr, errIngestionLeaseOwnershipLost) {
			t.Fatalf("expected errIngestionLeaseOwnershipLost, got %v", gotErr)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected ownership lost error from renew loop")
	}
}
