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

	"github.com/kapu/hololive-shared/pkg/panicguard"
)

// worker: 작업 실행 워커
func (s *Scheduler) worker(ctx context.Context, jobCh <-chan *Job, id int, stopCh <-chan struct{}) {
	defer s.wg.Done()

	for {
		job, ok := nextWorkerJob(ctx, jobCh, stopCh)
		if !ok {
			return
		}
		s.executeJobGuarded(ctx, job, id)
	}
}

// Poll panic이 여기서 recover되지 않으면 워커 goroutine이 영구 소실되고, heap에서
// pop된 이 job은 다시는 재등록되지 않는다 — recover를 오류로 바꿔 둘 다 살린다.
func (s *Scheduler) executeJobGuarded(ctx context.Context, job *Job, workerID int) {
	if err := panicguard.RunE(s.logger, "youtube-poll-job", func() error {
		s.executeJob(ctx, job, workerID)
		return nil
	}); err != nil {
		s.rescheduleJobAfterPoll(job, err)
	}
}

func nextWorkerJob(ctx context.Context, jobCh <-chan *Job, stopCh <-chan struct{}) (*Job, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	case <-stopCh:
		return nil, false
	case job, ok := <-jobCh:
		return validWorkerJob(job, ok)
	}
}

func validWorkerJob(job *Job, ok bool) (*Job, bool) {
	if !ok || job == nil {
		return nil, false
	}
	return job, true
}
