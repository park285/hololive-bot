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
package poller

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// worker: 작업 실행 워커
func (s *Scheduler) worker(ctx context.Context, jobCh <-chan *Job, id int, stopCh <-chan struct{}) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case job, ok := <-jobCh:
			if !ok {
				return
			}
			s.executeJob(ctx, job, id)
		}
	}
}

// executeJob: 작업 실행
func (s *Scheduler) executeJob(ctx context.Context, job *Job, workerID int) {
	// 레이트 리밋 대기
	if err := s.rateLimiter.Wait(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Debug("Rate limiter wait canceled", "error", err)
		} else {
			slog.Warn("Rate limiter wait failed", "error", err)
		}
		s.rescheduleJobAfterPoll(job, err)
		return
	}

	pollCtx := ctx
	cancel := func() {}
	if s.pollTimeout > 0 {
		pollCtx, cancel = context.WithTimeout(ctx, s.pollTimeout)
	}
	defer cancel()

	start := time.Now()
	err := job.Poller.Poll(pollCtx, job.ChannelID)
	elapsed := time.Since(start)
	status := "success"

	if err != nil {
		status = "error"
		if errors.Is(err, context.Canceled) {
			status = "canceled"
			slog.Debug("Poll canceled",
				"poller", job.Poller.Name(),
				"channel_id", job.ChannelID,
				"worker_id", workerID,
				"elapsed", elapsed)
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(pollCtx.Err(), context.DeadlineExceeded) {
			status = "timeout"
			slog.Warn("Poll timed out",
				"poller", job.Poller.Name(),
				"channel_id", job.ChannelID,
				"worker_id", workerID,
				"timeout", s.pollTimeout,
				"elapsed", elapsed,
				"error", err)
		} else {
			slog.Warn("Poll failed",
				"poller", job.Poller.Name(),
				"channel_id", job.ChannelID,
				"worker_id", workerID,
				"error", err,
				"elapsed", elapsed)
		}
	} else {
		slog.Debug("Poll succeeded",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"worker_id", workerID,
			"elapsed", elapsed)
	}
	schedulerPollDuration.WithLabelValues(job.Poller.Name(), status).Observe(elapsed.Seconds())

	s.rescheduleJobAfterPoll(job, err)
}
