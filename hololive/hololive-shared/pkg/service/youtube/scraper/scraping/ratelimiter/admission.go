package ratelimiter

import (
	"context"
	"fmt"
	"time"
)

type AdmissionDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Reason     string
}

const (
	AdmissionReasonLocalInterval        = "local_interval"
	AdmissionReasonDistributedRateLimit = "distributed_rate_limit"
)

func (r *RateLimiter) TryReserve(ctx context.Context) (AdmissionDecision, error) {
	return r.TryReserveWithBucket(ctx, "default")
}

func (r *RateLimiter) TryReserveWithBucket(ctx context.Context, bucket string) (AdmissionDecision, error) {
	bucket = normalizeBucket(bucket)

	reservation, localReserved, localDecision, err := r.tryReserveLocalAdmission(ctx)
	if err != nil || !localDecision.Allowed {
		return localDecision, err
	}

	distributedDecision, err := r.tryReserveDistributedAdmission(ctx, bucket)
	if err != nil || !distributedDecision.Allowed {
		if localReserved {
			r.rollbackLocalReservation(reservation)
		}
		return distributedDecision, err
	}

	return AdmissionDecision{Allowed: true}, nil
}

func normalizeBucket(bucket string) string {
	if bucket == "" {
		return "default"
	}
	return bucket
}

func (r *RateLimiter) tryReserveLocalAdmission(ctx context.Context) (localWaitReservation, bool, AdmissionDecision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return localWaitReservation{}, false, AdmissionDecision{}, fmt.Errorf("rate limiter admission canceled: %w", err)
	}

	now := time.Now()
	if r.interval <= 0 || r.lastTime.IsZero() {
		return r.reserveLocalAdmissionLocked(now), true, AdmissionDecision{Allowed: true}, nil
	}

	nextAllowed := r.lastTime.Add(r.interval)
	if !now.Before(nextAllowed) {
		return r.reserveLocalAdmissionLocked(now), true, AdmissionDecision{Allowed: true}, nil
	}

	return localWaitReservation{}, false, AdmissionDecision{
		Allowed:    false,
		RetryAfter: nextAllowed.Sub(now),
		Reason:     AdmissionReasonLocalInterval,
	}, nil
}

func (r *RateLimiter) reserveLocalAdmissionLocked(next time.Time) localWaitReservation {
	return r.commitLocalReservationLocked(next)
}

func (r *RateLimiter) tryReserveDistributedAdmission(ctx context.Context, bucket string) (AdmissionDecision, error) {
	if r.distributed == nil {
		return AdmissionDecision{Allowed: true}, nil
	}

	decision, err := r.distributed.Allow(ctx, bucket, r.distributedLimit, r.distributedWindow)
	if err != nil {
		return AdmissionDecision{}, fmt.Errorf("%w: distributed rate limiter allow failed: %w", ErrDistributedLimiterUnavailable, err)
	}
	if decision.Allowed {
		return AdmissionDecision{Allowed: true}, nil
	}
	if decision.RetryAfter <= 0 {
		return AdmissionDecision{}, fmt.Errorf("distributed rate limiter denied without retry_after")
	}
	return AdmissionDecision{
		Allowed:    false,
		RetryAfter: decision.RetryAfter,
		Reason:     AdmissionReasonDistributedRateLimit,
	}, nil
}
