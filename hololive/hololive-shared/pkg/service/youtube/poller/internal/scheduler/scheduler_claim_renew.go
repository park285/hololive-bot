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

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/cleanupctx"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

var errJobClaimRenewLoopStopped = errors.New("job claim renew loop stopped")

type jobClaimRenewController struct {
	pollCtx context.Context
	cancel  context.CancelFunc
	errCh   <-chan error
	done    <-chan struct{}
	active  bool

	stopOnce sync.Once
	stopErr  error
}

func inactiveJobClaimRenewController(ctx context.Context) *jobClaimRenewController {
	if ctx == nil {
		ctx = context.Background()
	}
	return &jobClaimRenewController{pollCtx: ctx, cancel: func() {}}
}

func (c *jobClaimRenewController) StopAndWait(parent context.Context, timeout time.Duration) error {
	if c == nil || !c.active {
		return nil
	}
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		c.stopErr = cleanupctx.Wait(parent, timeout, c.done)
	})
	return c.stopErr
}

func (c *jobClaimRenewController) Err() error {
	if c == nil {
		return nil
	}
	return drainJobClaimRenewError(c.errCh)
}

func (s *Scheduler) maybeStartJobClaimRenewLoop(
	ctx context.Context,
	pollerName string,
	decision jobClaimDecision,
) *jobClaimRenewController {
	if !decision.claimed {
		return inactiveJobClaimRenewController(ctx)
	}
	return s.startJobClaimRenewLoop(ctx, pollerName, decision.claim)
}

func (s *Scheduler) startJobClaimRenewLoop(ctx context.Context, pollerName string, claim polling.JobClaim) *jobClaimRenewController {
	if ctx == nil {
		ctx = context.Background()
	}
	pollCtx, pollCancel := context.WithCancel(ctx)
	renewCtx, renewCancel := context.WithCancel(context.WithoutCancel(ctx))
	errCh := make(chan error, 1)
	done := make(chan struct{})
	ttl := s.jobClaimLeaseTTL()
	interval := ttl / 3
	if interval <= 0 {
		interval = 20 * time.Second
	}

	metrics := s.metrics
	logger := s.logger
	panicguard.Go(logger, "youtube-poller-claim-renew", func() {
		defer close(done)
		err := panicguard.RunE(logger, "youtube-poller-claim-renew", func() error {
			runJobClaimRenewLoop(renewCtx, pollCtx, pollCancel, claim, pollerName, ttl, interval, errCh, metrics, logger)
			return nil
		})
		if err != nil {
			sendJobClaimRenewError(errCh, err)
			pollCancel()
		}
	})

	cancel := func() {
		renewCancel()
		pollCancel()
	}
	return &jobClaimRenewController{
		pollCtx: pollCtx,
		cancel:  cancel,
		errCh:   errCh,
		done:    done,
		active:  true,
	}
}

func runJobClaimRenewLoop(
	renewCtx context.Context,
	pollCtx context.Context,
	pollCancel context.CancelFunc,
	claim polling.JobClaim,
	pollerName string,
	ttl time.Duration,
	interval time.Duration,
	errCh chan<- error,
	metrics *polling.Metrics,
	logger *slog.Logger,
) {
	loopCtx, cancelLoop := context.WithCancel(renewCtx)
	stopPollCancel := context.AfterFunc(pollCtx, cancelLoop)
	defer func() {
		stopPollCancel()
		cancelLoop()
	}()

	if err := lifecycle.RunTickerLoop(loopCtx, interval, func(context.Context) error {
		if !renewJobClaim(pollCtx, pollCancel, claim, pollerName, ttl, errCh, metrics, logger) {
			return errJobClaimRenewLoopStopped
		}
		return nil
	}); err != nil {
		logger.Debug("Job claim renewal loop stopped", slog.Any("error", err))
	}
}

func renewJobClaim(
	ctx context.Context,
	cancel context.CancelFunc,
	claim polling.JobClaim,
	pollerName string,
	ttl time.Duration,
	errCh chan<- error,
	metrics *polling.Metrics,
	logger *slog.Logger,
) bool {
	renewed, err := claim.Renew(ctx, ttl)
	metrics.ObserveJobLeaseRenew(pollerName, polling.BoolResult(renewed, err))
	if err != nil {
		logger.Warn("job_lease_lost",
			slog.String("poller", pollerName),
			slog.String("result", "error"),
			slog.Any("error", err),
		)
		sendJobClaimRenewError(errCh, fmt.Errorf("renew poll job claim: %w", err))
		cancel()
		return false
	}
	if !renewed {
		logger.Warn("job_lease_lost",
			slog.String("poller", pollerName),
			slog.String("result", "lost"),
		)
		sendJobClaimRenewError(errCh, fmt.Errorf("renew poll job claim: ownership lost"))
		cancel()
		return false
	}
	return true
}

func sendJobClaimRenewError(errCh chan<- error, err error) {
	if errCh == nil || err == nil {
		return
	}
	select {
	case errCh <- err:
	default:
	}
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

func (s *Scheduler) finishJobClaim(ctx context.Context, job *Job, claim polling.JobClaim, pollErr error) error {
	completeCtx, cancel := cleanupctx.WithTimeout(ctx, s.claimCompletionTimeout)
	defer cancel()

	if pollErr == nil {
		completed, err := claim.MarkCompleted(completeCtx, job.Interval)
		s.metrics.ObserveJobMarkCompleted(job.Poller.Name(), polling.BoolResult(completed, err))
		s.logger.Debug("job_mark_completed",
			slog.String("poller", job.Poller.Name()),
			slog.String("result", polling.BoolResult(completed, err)),
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

func (s *Scheduler) releaseJobClaimWithCleanup(ctx context.Context, job *Job, claim polling.JobClaim) {
	cleanupCtx, cancel := cleanupctx.WithTimeout(ctx, s.claimCompletionTimeout)
	defer cancel()
	s.releaseJobClaim(cleanupCtx, job, claim)
}

func (s *Scheduler) releaseJobClaim(ctx context.Context, job *Job, claim polling.JobClaim) {
	released, err := claim.Release(ctx)
	s.metrics.ObserveJobRelease(job.Poller.Name(), polling.BoolResult(released, err))
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
