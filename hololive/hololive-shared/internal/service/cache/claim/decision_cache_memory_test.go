package claim

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryDecisionCache_ResolveClaimMissComputesStoresAndReturnsToken(t *testing.T) {
	t.Parallel()

	cache := NewMemoryDecisionCache()
	authorizedAt := time.Date(2026, 5, 22, 10, 30, 0, 0, time.UTC)
	computeCalls := 0

	result, err := cache.ResolveClaim(context.Background(), testDecisionKey(), func(ctx context.Context) (Decision, *Token, error) {
		computeCalls++
		return Decision{AuthorizedAt: authorizedAt, Value: "proceed"}, &Token{AuthorizedAt: authorizedAt}, nil
	})
	if err != nil {
		t.Fatalf("ResolveClaim() error = %v", err)
	}

	if result.Hit {
		t.Fatalf("ResolveClaim() Hit = true, want false")
	}
	if computeCalls != 1 {
		t.Fatalf("ResolveClaim() compute calls = %d, want 1", computeCalls)
	}
	if result.Decision.Value != "proceed" {
		t.Fatalf("ResolveClaim() decision value = %v, want %q", result.Decision.Value, "proceed")
	}
	if !result.Decision.AuthorizedAt.Equal(authorizedAt) {
		t.Fatalf("ResolveClaim() decision authorized_at = %s, want %s", result.Decision.AuthorizedAt, authorizedAt)
	}
	if result.Token == nil {
		t.Fatalf("ResolveClaim() token = nil, want non-nil")
	}
	if !result.Token.AuthorizedAt.Equal(authorizedAt) {
		t.Fatalf("ResolveClaim() token authorized_at = %s, want %s", result.Token.AuthorizedAt, authorizedAt)
	}
}

func TestMemoryDecisionCache_ResolveClaimMissCanReturnNilToken(t *testing.T) {
	t.Parallel()

	cache := NewMemoryDecisionCache()
	authorizedAt := time.Date(2026, 5, 22, 10, 30, 30, 0, time.UTC)

	result, err := cache.ResolveClaim(context.Background(), testDecisionKey(), func(ctx context.Context) (Decision, *Token, error) {
		return Decision{AuthorizedAt: authorizedAt, Value: "already-sent"}, nil, nil
	})
	if err != nil {
		t.Fatalf("ResolveClaim() error = %v", err)
	}

	if result.Hit {
		t.Fatalf("ResolveClaim() Hit = true, want false")
	}
	if result.Token != nil {
		t.Fatalf("ResolveClaim() token = %v, want nil", result.Token)
	}
	if result.Decision.Value != "already-sent" {
		t.Fatalf("ResolveClaim() decision value = %v, want %q", result.Decision.Value, "already-sent")
	}
	if !result.Decision.AuthorizedAt.Equal(authorizedAt) {
		t.Fatalf("ResolveClaim() decision authorized_at = %s, want %s", result.Decision.AuthorizedAt, authorizedAt)
	}
}

func TestMemoryDecisionCache_ResolveClaimHitSkipsComputeAndReturnsNilToken(t *testing.T) {
	t.Parallel()

	cache := NewMemoryDecisionCache()
	authorizedAt := time.Date(2026, 5, 22, 10, 30, 0, 0, time.UTC)
	if _, err := cache.ResolveClaim(context.Background(), testDecisionKey(), func(ctx context.Context) (Decision, *Token, error) {
		return Decision{AuthorizedAt: authorizedAt, Value: "already-sent"}, nil, nil
	}); err != nil {
		t.Fatalf("initial ResolveClaim() error = %v", err)
	}

	computeCalls := 0
	result, err := cache.ResolveClaim(context.Background(), testDecisionKey(), func(ctx context.Context) (Decision, *Token, error) {
		computeCalls++
		return Decision{AuthorizedAt: authorizedAt.Add(time.Second), Value: "retry-later"}, nil, nil
	})
	if err != nil {
		t.Fatalf("ResolveClaim() error = %v", err)
	}

	if !result.Hit {
		t.Fatalf("ResolveClaim() Hit = false, want true")
	}
	if computeCalls != 0 {
		t.Fatalf("ResolveClaim() compute calls = %d, want 0", computeCalls)
	}
	if result.Token != nil {
		t.Fatalf("ResolveClaim() token = %v, want nil", result.Token)
	}
	if result.Decision.Value != "already-sent" {
		t.Fatalf("ResolveClaim() decision value = %v, want %q", result.Decision.Value, "already-sent")
	}
	if !result.Decision.AuthorizedAt.Equal(authorizedAt) {
		t.Fatalf("ResolveClaim() decision authorized_at = %s, want %s", result.Decision.AuthorizedAt, authorizedAt)
	}
}

func TestMemoryDecisionCache_ResolveClaimComputeErrorDoesNotStore(t *testing.T) {
	t.Parallel()

	cache := NewMemoryDecisionCache()
	computeErr := errors.New("claim failed")
	firstCalls := 0

	result, err := cache.ResolveClaim(context.Background(), testDecisionKey(), func(ctx context.Context) (Decision, *Token, error) {
		firstCalls++
		return Decision{Value: "retry-later"}, nil, computeErr
	})
	if !errors.Is(err, computeErr) {
		t.Fatalf("ResolveClaim() error = %v, want %v", err, computeErr)
	}
	if result.Hit {
		t.Fatalf("ResolveClaim() Hit = true, want false")
	}
	if result.Token != nil {
		t.Fatalf("ResolveClaim() token = %v, want nil", result.Token)
	}
	if firstCalls != 1 {
		t.Fatalf("ResolveClaim() compute calls = %d, want 1", firstCalls)
	}

	authorizedAt := time.Date(2026, 5, 22, 10, 31, 0, 0, time.UTC)
	secondCalls := 0
	result, err = cache.ResolveClaim(context.Background(), testDecisionKey(), func(ctx context.Context) (Decision, *Token, error) {
		secondCalls++
		return Decision{AuthorizedAt: authorizedAt, Value: "proceed"}, &Token{AuthorizedAt: authorizedAt}, nil
	})
	if err != nil {
		t.Fatalf("second ResolveClaim() error = %v", err)
	}
	if result.Hit {
		t.Fatalf("second ResolveClaim() Hit = true, want false")
	}
	if secondCalls != 1 {
		t.Fatalf("second ResolveClaim() compute calls = %d, want 1", secondCalls)
	}
	if result.Token == nil {
		t.Fatalf("second ResolveClaim() token = nil, want non-nil")
	}
}

func TestMemoryDecisionCache_ResolveClaimConcurrentMissComputesOnce(t *testing.T) {
	t.Parallel()

	cache := NewMemoryDecisionCache()
	authorizedAt := time.Date(2026, 5, 22, 10, 32, 0, 0, time.UTC)
	const workers = 16

	var computeCalls atomic.Int64
	var hitCount atomic.Int64
	var missCount atomic.Int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for range workers {
		wg.Go(func() {
			resolveConcurrentClaim(start, cache, authorizedAt, &computeCalls, &hitCount, &missCount, errs)
		})
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("ResolveClaim() concurrent error = %v", err)
	}
	if computeCalls.Load() != 1 {
		t.Fatalf("ResolveClaim() compute calls = %d, want 1", computeCalls.Load())
	}
	if missCount.Load() != 1 {
		t.Fatalf("ResolveClaim() misses = %d, want 1", missCount.Load())
	}
	if hitCount.Load() != workers-1 {
		t.Fatalf("ResolveClaim() hits = %d, want %d", hitCount.Load(), workers-1)
	}
}

func resolveConcurrentClaim(start <-chan struct{}, cache *MemoryDecisionCache, authorizedAt time.Time, computeCalls, hitCount, missCount *atomic.Int64, errs chan<- error) {
	<-start
	result, err := cache.ResolveClaim(context.Background(), testDecisionKey(), func(ctx context.Context) (Decision, *Token, error) {
		computeCalls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return Decision{AuthorizedAt: authorizedAt, Value: "proceed"}, &Token{AuthorizedAt: authorizedAt}, nil
	})
	if err != nil {
		errs <- err
		return
	}
	if !result.Decision.AuthorizedAt.Equal(authorizedAt) {
		errs <- errors.New("unexpected authorized_at")
		return
	}
	if result.Hit {
		hitCount.Add(1)
		if result.Token != nil {
			errs <- errors.New("hit returned token")
		}
		return
	}
	missCount.Add(1)
	if result.Token == nil {
		errs <- errors.New("miss returned nil token")
	}
}

func testDecisionKey() string {
	return "youtube_outbox_delivery\x00short:video-1"
}
