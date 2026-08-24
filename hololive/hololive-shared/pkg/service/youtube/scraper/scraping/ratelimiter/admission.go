package ratelimiter

import (
	"context"
	"errors"
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
	out, err := r.TryReserveWithBucket(ctx, "default")
	if err != nil {
		return out, fmt.Errorf("try reserve with bucket: %w", err)
	}

	return out, nil
}

func (r *RateLimiter) TryReserveWithBucket(ctx context.Context, bucket string) (AdmissionDecision, error) {
	bucket = normalizeBucket(bucket)

	reservation, localReserved, localDecision, err := r.tryReserveLocalAdmission(ctx)
	if err != nil {
		return localDecision, fmt.Errorf("try reserve local admission: %w", err)
	}

	if !localDecision.Allowed {
		return localDecision, nil
	}

	distributedDecision, err := r.tryReserveDistributedAdmission(ctx, bucket)
	if err != nil || !distributedDecision.Allowed {
		if localReserved {
			r.rollbackLocalReservation(reservation)
		}

		if err != nil {
			return distributedDecision, fmt.Errorf("try reserve distributed admission: %w", err)
		}

		return distributedDecision, nil
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
		return AdmissionDecision{}, errors.New("distributed rate limiter denied without retry_after")
	}

	return AdmissionDecision{
		Allowed:    false,
		RetryAfter: decision.RetryAfter,
		Reason:     AdmissionReasonDistributedRateLimit,
	}, nil
}
