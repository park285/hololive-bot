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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_ZeroRequestInterval_NoopRL(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{RequestInterval: 0})
	ctx := context.Background()

	// 첫 호출은 초기화 역할을 하고, 두 번째 호출이 실제 무대기 경로를 검증한다.
	require.NoError(t, scheduler.rateLimiter.Wait(ctx))

	start := time.Now()
	err := scheduler.rateLimiter.Wait(ctx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, time.Millisecond, "zero interval rate limiter should not block")
}

func TestDispatchDueJobs_WorkerChannelFullKeepsAnchoredNextRunAt(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	scheduledAt := time.Now().Add(-time.Second).UTC().Truncate(time.Second)
	job := &Job{
		ChannelID: "channel-1",
		Poller:    &togglePollerStub{name: "videos"},
		Priority:  PriorityNormal,
		NextRunAt: scheduledAt,
		Interval:  5 * time.Minute,
	}
	job.index = 0
	scheduler.jobs = jobHeap{job}

	jobCh := make(chan *Job)
	retrySoon := scheduler.dispatchDueJobs(jobCh)

	require.Len(t, scheduler.jobs, 1)
	assert.Equal(t, scheduledAt, scheduler.jobs[0].NextRunAt)
	assert.True(t, retrySoon)
}

func TestSchedulerNextDispatchDelay_UsesShortRetryWhenWorkerChannelFull(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})

	delay := scheduler.nextDispatchDelay(true)
	assert.Equal(t, 50*time.Millisecond, delay)
}
