package scraper

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

type stubDistributedLimiter struct {
	mu        sync.Mutex
	decisions []ratelimit.Decision
}

func (s *stubDistributedLimiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (ratelimit.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.decisions) == 0 {
		return ratelimit.Decision{Allowed: true}, nil
	}
	if len(s.decisions) == 1 {
		return s.decisions[0], nil
	}

	d := s.decisions[0]
	s.decisions = s.decisions[1:]
	return d, nil
}

func TestRateLimiter_WaitWithBucket_DistributedDeniedThenAllowed(t *testing.T) {
	rl := NewRateLimiter(0)
	dist := &stubDistributedLimiter{
		decisions: []ratelimit.Decision{
			{Allowed: false, RetryAfter: 5 * time.Millisecond},
			{Allowed: true},
		},
	}
	if err := rl.ConfigureDistributed(dist, 1, time.Second); err != nil {
		t.Fatalf("configure distributed limiter: %v", err)
	}

	if err := rl.WaitWithBucket(context.Background(), "youtube:scraper:videos"); err != nil {
		t.Fatalf("wait with bucket: %v", err)
	}
}

func TestRateLimiter_WaitWithBucket_DistributedDeniedWithoutRetryAfter(t *testing.T) {
	rl := NewRateLimiter(0)
	dist := &stubDistributedLimiter{
		decisions: []ratelimit.Decision{
			{Allowed: false, RetryAfter: 0},
		},
	}
	if err := rl.ConfigureDistributed(dist, 1, time.Second); err != nil {
		t.Fatalf("configure distributed limiter: %v", err)
	}

	if err := rl.WaitWithBucket(context.Background(), "youtube:scraper:videos"); err == nil {
		t.Fatalf("expected error but got nil")
	}
}

func TestDistributedBucketFromURL(t *testing.T) {
	got := distributedBucketFromURL("https://www.youtube.com/channel/UC123/videos")
	want := "youtube:scraper:channel:UC123:videos"
	if got != want {
		t.Fatalf("bucket mismatch: got %q want %q", got, want)
	}
}
