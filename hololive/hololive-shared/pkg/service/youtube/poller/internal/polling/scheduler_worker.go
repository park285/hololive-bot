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
)

// worker: 작업 실행 워커
func (s *Scheduler) worker(ctx context.Context, jobCh <-chan *Job, id int, stopCh <-chan struct{}) {
	defer s.wg.Done()

	for {
		job, ok := nextWorkerJob(ctx, jobCh, stopCh)
		if !ok {
			return
		}
		s.executeJob(ctx, job, id)
	}
}

func nextWorkerJob(ctx context.Context, jobCh <-chan *Job, stopCh <-chan struct{}) (*Job, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	case <-stopCh:
		return nil, false
	case job, ok := <-jobCh:
		return job, ok
	}
}

// executeJob: 작업 실행
func (s *Scheduler) executeJob(ctx context.Context, job *Job, workerID int) {
	decision := s.claimJobRun(ctx, job)
	if decision.err != nil {
		s.rescheduleJobAfterPoll(job, decision.err)
		return
	}
	if !decision.proceed {
		return
	}

	if err := s.waitForJobRunSlot(ctx, job, decision); err != nil {
		return
	}

	pollCtx, cancel := s.pollContext(ctx)
	defer cancel()
	pollCtx, renewCancel, renewErrCh := s.maybeStartJobClaimRenewLoop(pollCtx, job.Poller.Name(), decision)
	defer renewCancel()

	start := time.Now()
	err := job.Poller.Poll(pollCtx, job.ChannelID)
	elapsed := time.Since(start)
	if renewErr := drainJobClaimRenewError(renewErrCh); renewErr != nil && err == nil {
		err = renewErr
	}
	if decision.claimed {
		err = s.finishJobClaim(ctx, job, decision.claim, err)
	}
	status := s.logPollResult(job, workerID, pollCtx, elapsed, err)
	schedulerPollDuration.WithLabelValues(job.Poller.Name(), status).Observe(elapsed.Seconds())

	s.rescheduleJobAfterPoll(job, err)
}

type jobClaimDecision struct {
	claim   JobClaim
	claimed bool
	proceed bool
	err     error
}

func (s *Scheduler) claimJobRun(ctx context.Context, job *Job) jobClaimDecision {
	if s.jobClaimer == nil {
		return jobClaimDecision{proceed: true}
	}
	leaseTTL := s.jobClaimLeaseTTL()
	status, claim, err := s.jobClaimer.TryClaim(ctx, job.Poller.Name(), job.ChannelID, leaseTTL, job.Interval)
	if err != nil {
		observeJobClaim(job.Poller.Name(), string(JobClaimUnavailable))
		slog.Warn("job_claim",
			slog.String("poller", job.Poller.Name()),
			slog.String("result", string(JobClaimUnavailable)),
			slog.Any("error", err),
		)
		return jobClaimDecision{err: fmt.Errorf("claim poll job: %w", err)}
	}
	observeJobClaim(job.Poller.Name(), string(status.Result))
	slog.Debug("job_claim",
		slog.String("poller", job.Poller.Name()),
		slog.String("result", string(status.Result)),
		slog.Duration("retry_after", status.RetryAfter),
		slog.Duration("lease_ttl", status.LeaseTTL),
	)
	return s.resolveJobClaimDecision(job, status, claim)
}

func (s *Scheduler) resolveJobClaimDecision(job *Job, status JobClaimStatus, claim JobClaim) jobClaimDecision {
	switch status.Result {
	case JobClaimAcquired:
		return acquiredJobClaimDecision(claim)
	case JobClaimPeerOwned, JobClaimAlreadyCompleted:
		s.rescheduleJobAfterClaimSkip(job, status.RetryAfter)
		return jobClaimDecision{}
	case JobClaimUnavailable:
		return jobClaimDecision{err: fmt.Errorf("claim poll job: unavailable")}
	default:
		return jobClaimDecision{err: fmt.Errorf("claim poll job: unknown result: %s", status.Result)}
	}
}

func acquiredJobClaimDecision(claim JobClaim) jobClaimDecision {
	if claim == nil {
		return jobClaimDecision{err: fmt.Errorf("claim poll job: acquired without claim handle")}
	}
	return jobClaimDecision{claim: claim, claimed: true, proceed: true}
}

func (s *Scheduler) waitForJobRunSlot(ctx context.Context, job *Job, decision jobClaimDecision) error {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		logRateLimiterWaitError(err)
		if decision.claimed {
			s.releaseJobClaim(ctx, job, decision.claim)
		}
		s.rescheduleJobAfterPoll(job, err)
		return err
	}
	return nil
}

func logRateLimiterWaitError(err error) {
	if errors.Is(err, context.Canceled) {
		slog.Debug("Rate limiter wait canceled", "error", err)
		return
	}
	slog.Warn("Rate limiter wait failed", "error", err)
}

func (s *Scheduler) pollContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.pollTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, s.pollTimeout)
}

func (s *Scheduler) jobClaimLeaseTTL() time.Duration {
	ttl := s.pollTimeout + 15*time.Second
	if ttl < time.Minute {
		return time.Minute
	}
	return ttl
}

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

	go func() {
		runJobClaimRenewLoop(renewCtx, pollCtx, pollCancel, claim, pollerName, ttl, interval, errCh)
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
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if !jobClaimRenewLoopStep(renewCtx, pollCtx, pollCancel, claim, pollerName, ttl, ticker.C, errCh) {
			return
		}
	}
}

func jobClaimRenewLoopStep(
	renewCtx context.Context,
	pollCtx context.Context,
	pollCancel context.CancelFunc,
	claim JobClaim,
	pollerName string,
	ttl time.Duration,
	ticks <-chan time.Time,
	errCh chan<- error,
) bool {
	select {
	case <-renewCtx.Done():
		return false
	case <-pollCtx.Done():
		return false
	case <-ticks:
		return renewJobClaim(pollCtx, pollCancel, claim, pollerName, ttl, errCh)
	}
}

func renewJobClaim(
	ctx context.Context,
	cancel context.CancelFunc,
	claim JobClaim,
	pollerName string,
	ttl time.Duration,
	errCh chan<- error,
) bool {
	renewed, err := claim.Renew(ctx, ttl)
	observeJobLeaseRenew(pollerName, boolResult(renewed, err))
	if err != nil {
		slog.Warn("job_lease_lost",
			slog.String("poller", pollerName),
			slog.String("result", "error"),
			slog.Any("error", err),
		)
		errCh <- fmt.Errorf("renew poll job claim: %w", err)
		cancel()
		return false
	}
	if !renewed {
		slog.Warn("job_lease_lost",
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
	if pollErr == nil {
		completed, err := claim.MarkCompleted(ctx, job.Interval)
		observeJobMarkCompleted(job.Poller.Name(), boolResult(completed, err))
		slog.Debug("job_mark_completed",
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

	s.releaseJobClaim(ctx, job, claim)
	return pollErr
}

func (s *Scheduler) releaseJobClaim(ctx context.Context, job *Job, claim JobClaim) {
	released, err := claim.Release(ctx)
	observeJobRelease(job.Poller.Name(), boolResult(released, err))
	if err != nil {
		slog.Warn("Poll job claim release failed",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"error", err)
		return
	}
	if !released {
		slog.Warn("Poll job claim release skipped after ownership loss",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID)
	}
}

func (s *Scheduler) logPollResult(job *Job, workerID int, pollCtx context.Context, elapsed time.Duration, err error) string {
	if err == nil {
		slog.Debug("Poll succeeded",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"worker_id", workerID,
			"elapsed", elapsed)
		return "success"
	}
	if errors.Is(err, context.Canceled) {
		slog.Debug("Poll canceled",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"worker_id", workerID,
			"elapsed", elapsed)
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(pollCtx.Err(), context.DeadlineExceeded) {
		s.logPollTimeout(job, workerID, elapsed, err)
		return "timeout"
	}
	slog.Warn("Poll failed",
		"poller", job.Poller.Name(),
		"channel_id", job.ChannelID,
		"worker_id", workerID,
		"error", err,
		"elapsed", elapsed)
	return "error"
}

func (s *Scheduler) logPollTimeout(job *Job, workerID int, elapsed time.Duration, err error) {
	slog.Warn("Poll timed out",
		"poller", job.Poller.Name(),
		"channel_id", job.ChannelID,
		"worker_id", workerID,
		"timeout", s.pollTimeout,
		"elapsed", elapsed,
		"error", err)
}
