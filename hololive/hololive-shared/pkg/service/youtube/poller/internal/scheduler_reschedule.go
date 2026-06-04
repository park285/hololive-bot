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
	"container/heap"
	"context"
	"errors"
	"time"
)

// rescheduleJob: 작업 재스케줄
func (s *Scheduler) rescheduleJob(job *Job) {
	s.rescheduleJobAfterPoll(job, nil)
}

type retryDelayError interface {
	RetryDelay() time.Duration
}

type JobSkipReason string

const (
	JobSkipPeerOwned        JobSkipReason = "peer_owned"
	JobSkipAlreadyCompleted JobSkipReason = "already_completed"
	JobSkipBudgetExhausted  JobSkipReason = "budget_exhausted"
	JobSkipSourceCooldown   JobSkipReason = "source_cooldown"
)

func (s *Scheduler) rescheduleJobAfterPoll(job *Job, pollErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job == nil || job.retired {
		return
	}
	current, ok := s.jobMap[job.key]
	if !ok || current != job {
		return
	}

	now := time.Now()
	s.updateJobNextRunAfterPoll(job, pollErr, now)

	if job.index >= 0 {
		heap.Fix(&s.jobs, job.index)
	} else {
		heap.Push(&s.jobs, job)
	}
	s.notifyDispatcher()
}

func (s *Scheduler) rescheduleJobAfterClaimSkip(job *Job, retryAfter time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job == nil || job.retired {
		return
	}
	current, ok := s.jobMap[job.key]
	if !ok || current != job {
		return
	}
	if retryAfter <= 0 {
		retryAfter = s.errorBackoffMin
	}

	job.consecutiveFailures = 0
	job.NextRunAt = time.Now().Add(retryAfter)
	if job.index >= 0 {
		heap.Fix(&s.jobs, job.index)
	} else {
		heap.Push(&s.jobs, job)
	}
	s.notifyDispatcher()
}

func (s *Scheduler) rescheduleJobAfterBudgetSkip(job *Job, retryAfter time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job == nil || job.retired {
		return
	}
	current, ok := s.jobMap[job.key]
	if !ok || current != job {
		return
	}
	if retryAfter <= 0 {
		retryAfter = s.errorBackoffMin
	}

	job.consecutiveFailures = 0
	job.NextRunAt = time.Now().Add(retryAfter)
	if job.index >= 0 {
		heap.Fix(&s.jobs, job.index)
	} else {
		heap.Push(&s.jobs, job)
	}
	s.logger.Info("Poll job rescheduled after budget skip",
		"poller", job.Poller.Name(),
		"channel_id", job.ChannelID,
		"reason", string(JobSkipBudgetExhausted),
		"retry_after", retryAfter,
		"next_run_at", job.NextRunAt)
	s.notifyDispatcher()
}

func (s *Scheduler) updateJobNextRunAfterPoll(job *Job, pollErr error, now time.Time) {
	if pollErr != nil && !errors.Is(pollErr, context.Canceled) {
		s.updateJobNextRunAfterFailure(job, pollErr, now)
		return
	}
	s.updateJobNextRunAfterSuccess(job, now)
}

func (s *Scheduler) updateJobNextRunAfterFailure(job *Job, pollErr error, now time.Time) {
	job.consecutiveFailures++

	var delayed retryDelayError
	if errors.As(pollErr, &delayed) && delayed.RetryDelay() > 0 {
		job.NextRunAt = now.Add(delayed.RetryDelay())
	} else {
		job.NextRunAt = nextErrorRetryAt(now, job.Interval, job.consecutiveFailures, s.errorBackoffMin, s.errorBackoffMax)
	}

	s.logger.Debug("Poll job rescheduled after failure",
		"poller", job.Poller.Name(),
		"channel_id", job.ChannelID,
		"consecutive_failures", job.consecutiveFailures,
		"next_run_at", job.NextRunAt)
}

func (s *Scheduler) updateJobNextRunAfterSuccess(job *Job, now time.Time) {
	hadFailures := job.consecutiveFailures > 0
	job.consecutiveFailures = 0

	if job.immediateFirstRun || hadFailures {
		job.NextRunAt = nextPollAt(now, job.Interval, job.Offset)
		job.immediateFirstRun = false
		return
	}
	job.NextRunAt = advanceNextRunAt(job.NextRunAt, job.Interval, now)
}
