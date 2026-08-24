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
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
)

func (s *Scheduler) jobClaimLeaseTTL() time.Duration {
	ttl := s.pollTimeout +
		s.budgetAcquireTimeout +
		s.claimCompletionTimeout +
		s.claimLeaseSafetyMargin
	if ttl < time.Minute {
		return time.Minute
	}

	return ttl
}

type jobBudgetReservation struct {
	reservation polling.BudgetReservation
	decision    polling.BudgetDecision
}

func (s *Scheduler) reserveJobBudget(ctx context.Context, job *Job) (jobBudgetReservation, error) {
	if s.budgetLimiter == nil || !s.budgetContext.Enabled || len(job.budgetProfile.SourceUnits) == 0 {
		return jobBudgetReservation{decision: polling.BudgetDecision{Allowed: true}}, nil
	}

	reserveCtx, cancel := context.WithTimeout(ctx, s.budgetAcquireTimeout)
	defer cancel()

	budgetJob := polling.BudgetJob{
		Namespace:  s.budgetContext.Namespace,
		InstanceID: s.budgetContext.InstanceID,
		PollerName: job.Poller.Name(),
		ChannelID:  job.ChannelID,
		JobKey:     job.key,
	}
	start := time.Now()
	reservation, decision, err := s.budgetLimiter.TryReserve(reserveCtx, &budgetJob, job.budgetProfile, s.jobClaimLeaseTTL())
	elapsed := time.Since(start)
	s.metrics.ObserveBudgetReserveWait(job.budgetProfile, elapsed)

	if err != nil {
		s.metrics.ObserveBudgetReserve(job.budgetProfile, "error")

		return jobBudgetReservation{}, fmt.Errorf("reserve poll job budget: %w", err)
	}

	if !decision.Allowed {
		s.metrics.ObserveBudgetReserve(job.budgetProfile, "denied")

		if decision.RetryAfter > 0 {
			s.metrics.ObserveBudgetRetryAfter(job.budgetProfile, decision.RetryAfter)
		}

		return jobBudgetReservation{decision: decision}, nil
	}

	s.metrics.ObserveBudgetReserve(job.budgetProfile, "allowed")

	if reservation != nil {
		s.metrics.AddBudgetInflight(job.budgetProfile, 1)
	}

	return jobBudgetReservation{reservation: reservation, decision: decision}, nil
}

func (s *Scheduler) logPollResult(pollCtx context.Context, job *Job, workerID int, elapsed time.Duration, err error) string {
	if err == nil {
		s.logger.Debug("Poll succeeded",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"worker_id", workerID,
			"elapsed", elapsed)

		return "success"
	}

	if isAdmissionDeferredPollError(err) {
		s.logger.Debug("Poll deferred",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"worker_id", workerID,
			"retry_after", admissionRetryAfterFromError(err, s.errorBackoffMin),
			"elapsed", elapsed,
			"error", err)

		return "deferred"
	}

	if errors.Is(err, context.Canceled) {
		s.logger.Debug("Poll canceled",
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

	s.logger.Warn("Poll failed",
		"poller", job.Poller.Name(),
		"channel_id", job.ChannelID,
		"worker_id", workerID,
		"error", err,
		"elapsed", elapsed)

	return "error"
}

func (s *Scheduler) logPollTimeout(job *Job, workerID int, elapsed time.Duration, err error) {
	s.logger.Warn("Poll timed out",
		"poller", job.Poller.Name(),
		"channel_id", job.ChannelID,
		"worker_id", workerID,
		"timeout", s.pollTimeout,
		"elapsed", elapsed,
		"error", err)
}
