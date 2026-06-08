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

// Package poller: YouTube 채널 데이터 폴링 및 스케줄링
package polling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

var errJobClaimRenewLoopStopped = errors.New("job claim renew loop stopped")

func (s *Scheduler) maybeStartJobClaimRenewLoop(
	ctx context.Context,
	pollerName string,
	decision jobClaimDecision,
) (context.Context, context.CancelFunc, <-chan error) {
	if !decision.claimed {
		return ctx, func() {}, nil
	}
	return s.startJobClaimRenewLoop(ctx, pollerName, decision.claim)
}

func (s *Scheduler) startJobClaimRenewLoop(ctx context.Context, pollerName string, claim JobClaim) (context.Context, context.CancelFunc, <-chan error) {
	pollCtx, pollCancel := context.WithCancel(ctx)
	renewCtx, renewCancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	ttl := s.jobClaimLeaseTTL()
	interval := ttl / 3
	if interval <= 0 {
		interval = 20 * time.Second
	}

	metrics := s.metrics
	logger := s.logger
	go func() {
		runJobClaimRenewLoop(renewCtx, pollCtx, pollCancel, claim, pollerName, ttl, interval, errCh, metrics, logger)
	}()

	cancel := func() {
		renewCancel()
		pollCancel()
	}
	return pollCtx, cancel, errCh
}

func runJobClaimRenewLoop(
	renewCtx context.Context,
	pollCtx context.Context,
	pollCancel context.CancelFunc,
	claim JobClaim,
	pollerName string,
	ttl time.Duration,
	interval time.Duration,
	errCh chan<- error,
	metrics *Metrics,
	logger *slog.Logger,
) {
	loopCtx, cancelLoop := context.WithCancel(renewCtx)
	stopPollCancel := context.AfterFunc(pollCtx, cancelLoop)
	defer func() {
		stopPollCancel()
		cancelLoop()
	}()

	_ = lifecycle.RunTickerLoop(loopCtx, interval, func(context.Context) error {
		if !renewJobClaim(pollCtx, pollCancel, claim, pollerName, ttl, errCh, metrics, logger) {
			return errJobClaimRenewLoopStopped
		}
		return nil
	})
}

func renewJobClaim(
	ctx context.Context,
	cancel context.CancelFunc,
	claim JobClaim,
	pollerName string,
	ttl time.Duration,
	errCh chan<- error,
	metrics *Metrics,
	logger *slog.Logger,
) bool {
	renewed, err := claim.Renew(ctx, ttl)
	metrics.ObserveJobLeaseRenew(pollerName, boolResult(renewed, err))
	if err != nil {
		logger.Warn("job_lease_lost",
			slog.String("poller", pollerName),
			slog.String("result", "error"),
			slog.Any("error", err),
		)
		errCh <- fmt.Errorf("renew poll job claim: %w", err)
		cancel()
		return false
	}
	if !renewed {
		logger.Warn("job_lease_lost",
			slog.String("poller", pollerName),
			slog.String("result", "lost"),
		)
		errCh <- fmt.Errorf("renew poll job claim: ownership lost")
		cancel()
		return false
	}
	return true
}

func drainJobClaimRenewError(errCh <-chan error) error {
	if errCh == nil {
		return nil
	}
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (s *Scheduler) finishJobClaim(ctx context.Context, job *Job, claim JobClaim, pollErr error) error {
	completeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.claimCompletionTimeout)
	defer cancel()

	if pollErr == nil {
		completed, err := claim.MarkCompleted(completeCtx, job.Interval)
		s.metrics.ObserveJobMarkCompleted(job.Poller.Name(), boolResult(completed, err))
		s.logger.Debug("job_mark_completed",
			slog.String("poller", job.Poller.Name()),
			slog.String("result", boolResult(completed, err)),
		)
		if err != nil {
			return fmt.Errorf("complete poll job claim: %w", err)
		}
		if !completed {
			return fmt.Errorf("complete poll job claim: ownership lost")
		}
		return nil
	}
	if isAdmissionDeferredPollError(pollErr) {
		return s.deferJobClaim(completeCtx, job, claim, pollErr)
	}

	s.releaseJobClaim(completeCtx, job, claim)
	return pollErr
}

func (s *Scheduler) releaseJobClaim(ctx context.Context, job *Job, claim JobClaim) {
	released, err := claim.Release(ctx)
	s.metrics.ObserveJobRelease(job.Poller.Name(), boolResult(released, err))
	if err != nil {
		s.logger.Warn("Poll job claim release failed",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"error", err)
		return
	}
	if !released {
		s.logger.Warn("Poll job claim release skipped after ownership loss",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID)
	}
}

func (s *Scheduler) observeJobLeaseElapsed(job *Job, elapsed time.Duration) {
	ttl := s.jobClaimLeaseTTL()
	if ttl <= 0 {
		return
	}
	ratio := elapsed.Seconds() / ttl.Seconds()
	s.metrics.ObserveJobLeaseElapsedRatio(job.Poller.Name(), ratio)
	if ratio <= 0.75 {
		return
	}
	s.metrics.ObserveJobLeaseNearExpiry(job.Poller.Name())
	s.logger.Warn("job_lease_near_expiry",
		slog.String("poller", job.Poller.Name()),
		slog.String("channel_id", job.ChannelID),
		slog.Duration("lease_elapsed", elapsed),
		slog.Duration("lease_ttl", ttl),
		slog.Float64("ratio", ratio),
	)
}
