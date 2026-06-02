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
	"time"
)

func (s *Scheduler) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}

	stopCh := make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	s.stopCh = stopCh
	s.stopCancel = cancel
	s.running = true

	workerCount := s.workerCount
	jobCount := len(s.jobs)
	pollTimeout := s.pollTimeout
	errorBackoffMin := s.errorBackoffMin
	errorBackoffMax := s.errorBackoffMax
	s.mu.Unlock()

	s.logger.Info("Scheduler starting",
		"worker_count", workerCount,
		"job_count", jobCount,
		"poll_timeout", pollTimeout,
		"error_backoff_min", errorBackoffMin,
		"error_backoff_max", errorBackoffMax)

	// 작업 채널
	jobCh := make(chan *Job, workerCount*2)

	// 워커 시작
	for i := range workerCount {
		s.wg.Add(1)
		go s.worker(runCtx, jobCh, i, stopCh)
	}

	// 디스패처 시작
	s.wg.Add(1)
	go s.dispatcher(runCtx, jobCh, stopCh)
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	stopCh := s.stopCh
	stopCancel := s.stopCancel
	s.running = false
	s.stopCancel = nil
	s.mu.Unlock()

	if stopCancel != nil {
		stopCancel()
	}
	if stopCh != nil {
		close(stopCh)
	}
	s.wg.Wait()
	s.logger.Info("Scheduler stopped")
}

func (s *Scheduler) NudgeAllJobs() {
	s.mu.Lock()
	now := time.Now()
	changed := false
	for _, job := range s.jobMap {
		if job == nil || job.retired {
			continue
		}
		job.consecutiveFailures = 0
		job.NextRunAt = now
		if job.index >= 0 {
			heap.Fix(&s.jobs, job.index)
		}
		changed = true
	}
	s.mu.Unlock()
	if changed {
		s.notifyDispatcher()
	}
}

func (s *Scheduler) notifyDispatcher() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}
