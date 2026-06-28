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
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/lease"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

func newTestCacheForLock(t *testing.T) *cache.Service {
	t.Helper()
	return testutil.NewTestCacheService(t, context.Background())
}

func acquireTestLease(t *testing.T, cacheClient cache.Client, ttl, renewGap time.Duration) *Lease {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	owner := newOwnerToken("bot")
	inner, err := lease.Acquire(context.Background(), cacheClient, &lease.Spec{
		Name:     "ingestion",
		Key:      Key,
		Owner:    owner,
		TTL:      ttl,
		RenewGap: renewGap,
	}, logger)
	if err != nil {
		t.Fatalf("acquire test lease: %v", err)
	}
	return &Lease{inner: inner, key: Key, owner: owner, role: "bot", logger: logger}
}

func TestAcquireExclusive(t *testing.T) {
	var held bool
	var owner string
	cacheClient := &cachemocks.Client{
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

	first, err := Acquire(context.Background(), cacheClient, "bot", logger)
	if err != nil {
		t.Fatalf("first lease: %v", err)
	}

	if _, err := Acquire(context.Background(), cacheClient, "youtube-producer", logger); err == nil {
		t.Fatalf("expected second acquisition to fail")
	}

	if err := first.Release(context.Background()); err != nil {
		t.Fatalf("release first lease: %v", err)
	}

	if _, err := Acquire(context.Background(), cacheClient, "youtube-producer", logger); err != nil {
		t.Fatalf("lease after release should succeed: %v", err)
	}
}

func TestLeaseRenewLoop(t *testing.T) {
	cacheService := newTestCacheForLock(t)
	l := acquireTestLease(t, cacheService, time.Second, 200*time.Millisecond)

	go l.StartRenewLoop(t.Context(), nil)

	time.Sleep(1300 * time.Millisecond)

	exists, err := cacheService.Exists(context.Background(), Key)
	if err != nil {
		t.Fatalf("exists check: %v", err)
	}
	if !exists {
		t.Fatalf("lease key should still exist due to renew loop")
	}
}

func TestLeaseRenewLoopReportsOwnershipLost(t *testing.T) {
	cacheService := newTestCacheForLock(t)
	l := acquireTestLease(t, cacheService, time.Second, 20*time.Millisecond)

	errCh := make(chan error, 1)
	go l.StartRenewLoop(t.Context(), errCh)

	if err := cacheService.GetClient().Do(context.Background(), cacheService.B().Set().Key(Key).Value("other-owner").Build()).Error(); err != nil {
		t.Fatalf("override lock owner: %v", err)
	}

	select {
	case gotErr := <-errCh:
		if !errors.Is(gotErr, lease.ErrOwnershipLost) {
			t.Fatalf("expected ownership lost error, got %v", gotErr)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected ownership lost error from renew loop")
	}
}
