package ratelimiter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

type admissionStubDistributedLimiter struct {
	mu        sync.Mutex
	decisions []ratelimit.Decision
}

func (s *admissionStubDistributedLimiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (ratelimit.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.decisions) == 0 {
		return ratelimit.Decision{Allowed: true}, nil
	}
	if len(s.decisions) == 1 {
		return s.decisions[0], nil
	}

	decision := s.decisions[0]
	s.decisions = s.decisions[1:]
	return decision, nil
}

func TestRateLimiterTryReserveWithBucket_LocalDeniedDoesNotBlock(t *testing.T) {
	limiter := New(time.Hour)
	if decision, err := limiter.TryReserveWithBucket(context.Background(), "bucket"); err != nil || !decision.Allowed {
		t.Fatalf("first reserve = (%+v, %v), want allowed", decision, err)
	}

	start := time.Now()
	decision, err := limiter.TryReserveWithBucket(context.Background(), "bucket")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("second reserve returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("second reserve allowed unexpectedly")
	}
	if decision.Reason != AdmissionReasonLocalInterval {
		t.Fatalf("reason = %q, want %q", decision.Reason, AdmissionReasonLocalInterval)
	}
	if decision.RetryAfter <= 0 || decision.RetryAfter > time.Hour {
		t.Fatalf("retry_after = %s, want within (0, 1h]", decision.RetryAfter)
	}
	if elapsed > 20*time.Millisecond {
		t.Fatalf("TryReserveWithBucket blocked for %s", elapsed)
	}
}

func TestRateLimiterTryReserveWithBucket_DistributedDeniedRollsBackLocalReservation(t *testing.T) {
	limiter := New(time.Hour)
	distributed := &admissionStubDistributedLimiter{
		decisions: []ratelimit.Decision{
			{Allowed: false, RetryAfter: 5 * time.Second},
			{Allowed: true},
		},
	}
	if err := limiter.ConfigureDistributed(distributed, 1, time.Second); err != nil {
		t.Fatalf("configure distributed limiter: %v", err)
	}

	decision, err := limiter.TryReserveWithBucket(context.Background(), "bucket")
	if err != nil {
		t.Fatalf("first reserve returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("first reserve allowed unexpectedly")
	}
	if decision.Reason != AdmissionReasonDistributedRateLimit {
		t.Fatalf("reason = %q, want %q", decision.Reason, AdmissionReasonDistributedRateLimit)
	}

	decision, err = limiter.TryReserveWithBucket(context.Background(), "bucket")
	if err != nil {
		t.Fatalf("second reserve returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("second reserve denied after rollback: %+v", decision)
	}
}

func TestRateLimiterTryReserveWithBucket_DistributedAllowedConsumesLocalSlot(t *testing.T) {
	limiter := New(time.Hour)
	distributed := &admissionStubDistributedLimiter{decisions: []ratelimit.Decision{{Allowed: true}}}
	if err := limiter.ConfigureDistributed(distributed, 1, time.Second); err != nil {
		t.Fatalf("configure distributed limiter: %v", err)
	}

	decision, err := limiter.TryReserveWithBucket(context.Background(), "bucket")
	if err != nil || !decision.Allowed {
		t.Fatalf("first reserve = (%+v, %v), want allowed", decision, err)
	}
	decision, err = limiter.TryReserveWithBucket(context.Background(), "bucket")
	if err != nil {
		t.Fatalf("second reserve returned error: %v", err)
	}
	if decision.Allowed || decision.Reason != AdmissionReasonLocalInterval {
		t.Fatalf("second reserve = %+v, want local denial", decision)
	}
}
