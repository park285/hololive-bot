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
	"container/heap"
	"context"
	"time"
)

type dispatcherSignal int

const (
	dispatcherSignalStop dispatcherSignal = iota
	dispatcherSignalWake
	dispatcherSignalTimer
)

// dispatcher: 실행 대기 작업을 워커에게 전달
func (s *Scheduler) dispatcher(ctx context.Context, jobCh chan<- *Job, stopCh <-chan struct{}) {
	defer s.wg.Done()
	defer close(jobCh)

	timer := time.NewTimer(0)
	defer timer.Stop()
	workerChannelFull := false
	doneCh, cancelDone := dispatcherDoneChannel(ctx, stopCh)
	defer cancelDone()

	for {
		s.resetDispatchTimer(timer, workerChannelFull)

		signal := s.awaitDispatcherSignal(doneCh, timer)
		if signal == dispatcherSignalStop {
			return
		}
		if signal == dispatcherSignalWake {
			workerChannelFull = false
			continue
		}
		if signal == dispatcherSignalTimer {
			workerChannelFull = s.dispatchDueJobs(jobCh)
		}
	}
}

func dispatcherDoneChannel(ctx context.Context, stopCh <-chan struct{}) (<-chan struct{}, context.CancelFunc) {
	dispatchCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-stopCh:
			cancel()
		case <-dispatchCtx.Done():
		}
	}()
	return dispatchCtx.Done(), cancel
}

func (s *Scheduler) awaitDispatcherSignal(doneCh <-chan struct{}, timer *time.Timer) dispatcherSignal {
	select {
	case <-doneCh:
		return dispatcherSignalStop
	case <-s.wakeCh:
		return dispatcherSignalWake
	case <-timer.C:
		return dispatcherSignalTimer
	}
}

func (s *Scheduler) resetDispatchTimer(timer *time.Timer, workerChannelFull bool) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(s.nextDispatchDelay(workerChannelFull))
}

func (s *Scheduler) nextDispatchDelay(workerChannelFull bool) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if workerChannelFull {
		return 50 * time.Millisecond
	}
	if len(s.jobs) == 0 {
		return time.Second
	}

	wait := time.Until(s.jobs[0].NextRunAt)
	if wait < 0 {
		return 0
	}
	return wait
}

// dispatchDueJobs: 실행 시간이 된 작업 전달
func (s *Scheduler) dispatchDueJobs(jobCh chan<- *Job) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for len(s.jobs) > 0 {
		job := s.jobs[0]
		if job.NextRunAt.After(now) {
			break
		}

		// 작업 추출
		heap.Pop(&s.jobs)

		// 워커에게 전달 (논블로킹)
		select {
		case jobCh <- job:
		default:
			// 채널 가득 참 - 현재 슬롯 anchor를 유지한 채 재시도한다.
			s.metrics.SchedulerDispatchDefer.WithLabelValues("worker_channel_full").Inc()
			heap.Push(&s.jobs, job)
			return true
		}
	}

	return false
}
