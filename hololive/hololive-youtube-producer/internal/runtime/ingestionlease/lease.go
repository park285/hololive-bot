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
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/park285/hololive-bot/shared-go/pkg/backoff"
)

const (
	Key                    = "lock:ingestion:runtime"
	ingestionLeaseTTL      = 2 * time.Minute
	ingestionLeaseRenewGap = 40 * time.Second
)

var errIngestionLeaseOwnershipLost = errors.New("ingestion lease ownership lost")

type Lease struct {
	cacheClient   cache.Client
	key           string
	owner         string
	role          string
	ttl           time.Duration
	renewInterval time.Duration
	logger        *slog.Logger
	retrySleep    func(ctx context.Context, d time.Duration) bool
}

func Acquire(
	ctx context.Context,
	cacheClient cache.Client,
	role string,
	logger *slog.Logger,
) (*Lease, error) {
	if cacheClient == nil {
		return nil, fmt.Errorf("acquire ingestion lease: cache service must not be nil")
	}
	if role == "" {
		return nil, fmt.Errorf("acquire ingestion lease: role must not be empty")
	}
	if logger == nil {
		logger = slog.Default()
	}

	owner := fmt.Sprintf("%s:%d:%d", role, os.Getpid(), time.Now().UnixNano())
	acquired, err := cacheClient.SetNX(ctx, Key, owner, ingestionLeaseTTL)
	if err != nil {
		return nil, fmt.Errorf("acquire ingestion lease: setnx failed: %w", err)
	}
	if !acquired {
		return nil, fmt.Errorf("acquire ingestion lease: lock already held: key=%s", Key)
	}

	logger.Info("Ingestion lease acquired",
		slog.String("event", "ingestion_lease_acquired"),
		slog.String("role", role),
		slog.String("key", Key),
		slog.String("owner", owner),
	)

	return &Lease{
		cacheClient:   cacheClient,
		key:           Key,
		owner:         owner,
		role:          role,
		ttl:           ingestionLeaseTTL,
		renewInterval: ingestionLeaseRenewGap,
		logger:        logger,
	}, nil
}

func (l *Lease) StartRenewLoop(ctx context.Context, errCh chan<- error) {
	if l == nil {
		return
	}

	renewInterval := l.renewInterval
	if renewInterval <= 0 {
		renewInterval = ingestionLeaseRenewGap
	}

	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		if l.waitAndRenew(ctx, ticker.C, errCh) {
			return
		}
	}
}

func (l *Lease) waitAndRenew(ctx context.Context, ticks <-chan time.Time, errCh chan<- error) bool {
	select {
	case <-ctx.Done():
		return true
	case <-ticks:
		return l.renewOnce(ctx, errCh)
	}
}

func (l *Lease) renewOnce(ctx context.Context, errCh chan<- error) bool {
	if err := l.renew(ctx); err != nil {
		return l.handleRenewError(errCh, err)
	}
	return false
}

func (l *Lease) handleRenewError(errCh chan<- error, err error) bool {
	if errors.Is(err, errIngestionLeaseOwnershipLost) {
		l.logger.Error("Ingestion lease ownership lost",
			slog.String("event", "ingestion_lease_lost"),
			slog.String("role", l.role),
			slog.String("key", l.key),
			slog.String("owner", l.owner),
			slog.Any("error", err),
		)
		l.reportRenewLoopError(errCh, fmt.Errorf("ingestion lease ownership lost: %w", err))
		return true
	}

	l.logger.Error("Ingestion lease renew exhausted all retries",
		slog.String("event", "ingestion_lease_renew_failed"),
		slog.String("role", l.role),
		slog.String("key", l.key),
		slog.Any("error", err),
	)
	l.reportRenewLoopError(errCh, fmt.Errorf("ingestion lease renew failed: %w", err))
	return true
}

func (l *Lease) reportRenewLoopError(errCh chan<- error, err error) {
	if errCh == nil {
		return
	}

	select {
	case errCh <- err:
	default:
	}
}

func (l *Lease) renew(ctx context.Context) error {
	err := withRetry(ctx, leaseRetryOptions{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Second,
		Jitter:      500 * time.Millisecond,
		Sleep:       l.retrySleep,
		ShouldRetry: func(err error) bool {
			return !errors.Is(err, errIngestionLeaseOwnershipLost)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			l.logger.Warn("Ingestion lease renew retrying",
				slog.String("event", "ingestion_lease_renew_retry"),
				slog.String("key", l.key),
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
				slog.Any("error", err),
			)
		},
	}, func(ctx context.Context) error {
		renewed, err := l.cacheClient.CompareAndExpire(ctx, l.key, l.owner, l.ttl)
		if err != nil {
			return fmt.Errorf("renew ingestion lease: %w", err)
		}
		if !renewed {
			return fmt.Errorf("renew ingestion lease: %w: key=%s", errIngestionLeaseOwnershipLost, l.key)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("renew ingestion lease with retry: %w", err)
	}

	return nil
}

type leaseRetryOptions struct {
	MaxAttempts int
	BaseDelay   time.Duration
	Jitter      time.Duration
	ShouldRetry func(error) bool
	OnRetry     func(int, error, time.Duration)
	Sleep       func(context.Context, time.Duration) bool
}

func withRetry(ctx context.Context, opts leaseRetryOptions, fn func(context.Context) error) error {
	opts = normalizeLeaseRetryOptions(opts)

	var lastErr error
	for attempt := range opts.MaxAttempts {
		err, done := leaseRetryAttempt(ctx, opts, attempt, lastErr, fn)
		if done {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func leaseRetryAttempt(
	ctx context.Context,
	opts leaseRetryOptions,
	attempt int,
	lastErr error,
	fn func(context.Context) error,
) (error, bool) {
	if err := leaseRetryContextError(ctx, lastErr); err != nil {
		return err, true
	}
	err := fn(ctx)
	if err == nil {
		return nil, true
	}
	if leaseRetryFinished(opts, attempt, err) {
		return err, true
	}
	if !leaseSleepBeforeRetry(ctx, opts, attempt, err) {
		return err, true
	}
	return err, false
}

func leaseRetryFinished(opts leaseRetryOptions, attempt int, err error) bool {
	return !leaseRetryShouldContinue(opts, err) || attempt >= opts.MaxAttempts-1
}

func normalizeLeaseRetryOptions(opts leaseRetryOptions) leaseRetryOptions {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepWithContext
	}
	return opts
}

func leaseRetryContextError(ctx context.Context, lastErr error) error {
	if ctx.Err() == nil {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("context error: %w", ctx.Err())
}

func leaseRetryShouldContinue(opts leaseRetryOptions, err error) bool {
	return opts.ShouldRetry == nil || opts.ShouldRetry(err)
}

func leaseSleepBeforeRetry(ctx context.Context, opts leaseRetryOptions, attempt int, err error) bool {
	delay := leaseBackoffDelay(attempt, opts.BaseDelay, opts.Jitter)
	if opts.OnRetry != nil {
		opts.OnRetry(attempt+1, err, delay)
	}
	return opts.Sleep(ctx, delay)
}

func leaseBackoffDelay(attempt int, baseDelay, jitter time.Duration) time.Duration {
	return backoff.ComputeExponentialBackoff(attempt, baseDelay, 0, jitter)
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (l *Lease) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}

	released, err := l.cacheClient.CompareAndDelete(ctx, l.key, l.owner)
	if err != nil {
		return fmt.Errorf("release ingestion lease: compare-and-delete failed: %w", err)
	}
	if !released {
		return fmt.Errorf("release ingestion lease: lease ownership mismatch")
	}

	l.logger.Info("Ingestion lease released",
		slog.String("event", "ingestion_lease_released"),
		slog.String("role", l.role),
		slog.String("key", l.key),
		slog.String("owner", l.owner),
	)
	return nil
}
