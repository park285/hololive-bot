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

package poller

import (
	"container/heap"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type togglePollerStub struct {
	name    string
	enabled bool
}

func (p *togglePollerStub) Poll(context.Context, string) error { return nil }
func (p *togglePollerStub) Name() string                       { return p.name }
func (p *togglePollerStub) SetProxyEnabled(enabled bool) bool {
	p.enabled = enabled
	return true
}
func (p *togglePollerStub) ProxyEnabled() bool { return p.enabled }

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

func TestScheduler_SetProxyEnabled(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{RequestInterval: 0})
	poller := &togglePollerStub{name: "toggle"}

	scheduler.Register("channel-1", poller, PriorityNormal, time.Minute)
	scheduler.Register("channel-2", poller, PriorityNormal, time.Minute) // 동일 poller 중복 등록

	applied := scheduler.SetProxyEnabled(true)
	assert.Equal(t, 1, applied)

	enabled, known := scheduler.ProxyEnabled()
	assert.True(t, known)
	assert.True(t, enabled)
}

func TestNextPollAt_KeepsAnchor(t *testing.T) {
	now := time.Date(2026, time.April, 9, 10, 2, 10, 0, time.UTC)
	interval := 5 * time.Minute
	offset := 2 * time.Minute

	got := nextPollAt(now, interval, offset)
	want := time.Date(2026, time.April, 9, 10, 7, 0, 0, time.UTC)

	assert.Equal(t, want, got)
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

func TestSchedulerRegisterSignalsWakeChannel(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})

	scheduler.Register("channel-1", &togglePollerStub{name: "videos"}, PriorityNormal, time.Minute)

	select {
	case <-scheduler.wakeCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected scheduler register to signal wakeCh")
	}
}

func TestSchedulerSyncPollerTargetsAddsAndRemovesJobs(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-old", p, PriorityNormal, time.Minute)

	scheduler.SyncPollerTargets(PollerTargetSync{
		Poller:     p,
		Priority:   PriorityHigh,
		Interval:   2 * time.Minute,
		ChannelIDs: []string{"channel-new"},
	})

	require.NotContains(t, scheduler.jobMap, "channel-old:videos")
	require.Contains(t, scheduler.jobMap, "channel-new:videos")
	assert.Equal(t, PriorityHigh, scheduler.jobMap["channel-new:videos"].Priority)
	assert.Equal(t, 2*time.Minute, scheduler.jobMap["channel-new:videos"].Interval)
}

func TestSchedulerSyncPollerTargetsRetiresInflightJobWithoutRequeue(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-old", p, PriorityNormal, time.Minute)

	job := scheduler.jobMap["channel-old:videos"]
	require.NotNil(t, job)
	heap.Pop(&scheduler.jobs)
	require.Equal(t, -1, job.index)

	scheduler.SyncPollerTargets(PollerTargetSync{
		Poller:     p,
		Priority:   PriorityNormal,
		Interval:   time.Minute,
		ChannelIDs: nil,
	})

	scheduler.rescheduleJob(job)

	require.NotContains(t, scheduler.jobMap, "channel-old:videos")
	require.Len(t, scheduler.jobs, 0)
}

func TestSchedulerSyncPollerTargetsForceImmediateFirstRunOnlyForNewJobs(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-existing", p, PriorityNormal, time.Hour)

	existing := scheduler.jobMap["channel-existing:videos"]
	require.NotNil(t, existing)
	existingNextRunAt := existing.NextRunAt

	before := time.Now()
	scheduler.SyncPollerTargets(PollerTargetSync{
		Poller:                 p,
		Priority:               PriorityHigh,
		Interval:               time.Hour,
		ChannelIDs:             []string{"channel-existing", "channel-new"},
		ForceImmediateFirstRun: true,
	})
	after := time.Now()

	require.Contains(t, scheduler.jobMap, "channel-new:videos")
	assert.Equal(t, existingNextRunAt, scheduler.jobMap["channel-existing:videos"].NextRunAt)

	newJob := scheduler.jobMap["channel-new:videos"]
	require.NotNil(t, newJob)
	assert.False(t, newJob.NextRunAt.Before(before))
	assert.False(t, newJob.NextRunAt.After(after))
}

func TestSchedulerRescheduleJobReanchorsAfterImmediateFirstRun(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}

	scheduler.SyncPollerTargets(PollerTargetSync{
		Poller:                 p,
		Priority:               PriorityNormal,
		Interval:               time.Hour,
		ChannelIDs:             []string{"channel-new"},
		ForceImmediateFirstRun: true,
	})

	job := scheduler.jobMap["channel-new:videos"]
	require.NotNil(t, job)
	require.True(t, job.immediateFirstRun)

	before := time.Now()
	scheduler.rescheduleJob(job)
	after := time.Now()

	assert.False(t, job.immediateFirstRun)
	lowerBound := nextPollAt(before, job.Interval, job.Offset)
	upperBound := nextPollAt(after, job.Interval, job.Offset)
	assert.False(t, job.NextRunAt.Before(lowerBound))
	assert.False(t, job.NextRunAt.After(upperBound))
}

func TestAdvanceNextRunAt_PreservesAnchorAcrossBacklog(t *testing.T) {
	scheduledAt := time.Date(2026, time.April, 9, 10, 7, 0, 0, time.UTC)
	now := time.Date(2026, time.April, 9, 10, 17, 30, 0, time.UTC)

	got := advanceNextRunAt(scheduledAt, 5*time.Minute, now)
	want := time.Date(2026, time.April, 9, 10, 22, 0, 0, time.UTC)

	assert.Equal(t, want, got)
}

type errorPollerStub struct {
	name string
	err  error
}

func (p *errorPollerStub) Poll(context.Context, string) error { return p.err }
func (p *errorPollerStub) Name() string                       { return p.name }

func TestSchedulerRescheduleJobBacksOffAfterPollFailure(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		ErrorBackoffMin: 10 * time.Second,
		ErrorBackoffMax: time.Minute,
	})
	p := &errorPollerStub{name: "videos", err: assert.AnError}

	scheduler.Register("channel-failing", p, PriorityNormal, time.Hour)
	job := scheduler.jobMap["channel-failing:videos"]
	require.NotNil(t, job)

	heap.Remove(&scheduler.jobs, job.index)
	require.Equal(t, -1, job.index)

	before := time.Now()
	scheduler.rescheduleJobAfterPoll(job, assert.AnError)
	after := time.Now()

	require.Equal(t, 1, job.consecutiveFailures)
	require.Len(t, scheduler.jobs, 1)
	assert.False(t, job.NextRunAt.Before(before.Add(10*time.Second)))
	assert.False(t, job.NextRunAt.After(after.Add(10*time.Second+100*time.Millisecond)))
}

func TestSchedulerRescheduleJobReanchorsAfterFailureRecovery(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		ErrorBackoffMin: 10 * time.Second,
		ErrorBackoffMax: time.Minute,
	})
	p := &errorPollerStub{name: "videos", err: assert.AnError}

	scheduler.Register("channel-recovered", p, PriorityNormal, time.Hour)
	job := scheduler.jobMap["channel-recovered:videos"]
	require.NotNil(t, job)

	heap.Remove(&scheduler.jobs, job.index)
	require.Equal(t, -1, job.index)

	scheduler.rescheduleJobAfterPoll(job, assert.AnError)
	heap.Remove(&scheduler.jobs, job.index)
	require.Equal(t, 1, job.consecutiveFailures)

	before := time.Now()
	scheduler.rescheduleJobAfterPoll(job, nil)
	after := time.Now()

	require.Equal(t, 0, job.consecutiveFailures)
	lowerBound := nextPollAt(before, job.Interval, job.Offset)
	upperBound := nextPollAt(after, job.Interval, job.Offset)
	assert.False(t, job.NextRunAt.Before(lowerBound))
	assert.False(t, job.NextRunAt.After(upperBound))
}

func TestSchedulerSyncPollerTargetsIntervalIncreaseRecalculatesOffsetAndClearsFailureBackoffSchedule(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		ErrorBackoffMin: 10 * time.Second,
		ErrorBackoffMax: time.Minute,
	})
	p := &errorPollerStub{name: "videos", err: assert.AnError}

	scheduler.Register("channel-reset", p, PriorityNormal, time.Hour)
	job := scheduler.jobMap["channel-reset:videos"]
	require.NotNil(t, job)
	originalOffset := job.Offset

	heap.Remove(&scheduler.jobs, job.index)
	require.Equal(t, -1, job.index)

	scheduler.rescheduleJobAfterPoll(job, assert.AnError)
	require.Equal(t, 1, job.consecutiveFailures)

	staleFailureNextRunAt := job.NextRunAt
	newInterval := 2 * time.Hour
	wantOffset := calculateOffset(job.key, newInterval)

	before := time.Now()
	scheduler.SyncPollerTargets(PollerTargetSync{
		Poller:     p,
		Priority:   PriorityHigh,
		Interval:   newInterval,
		ChannelIDs: []string{"channel-reset"},
	})
	after := time.Now()

	require.Equal(t, 0, job.consecutiveFailures)
	require.Equal(t, newInterval, job.Interval)
	require.NotEqual(t, originalOffset, job.Offset)
	require.Equal(t, wantOffset, job.Offset)
	assert.NotEqual(t, staleFailureNextRunAt, job.NextRunAt)

	lowerBound := nextPollAt(before, newInterval, wantOffset)
	upperBound := nextPollAt(after, newInterval, wantOffset)
	assert.False(t, job.NextRunAt.Before(lowerBound))
	assert.False(t, job.NextRunAt.After(upperBound))
}

func TestSchedulerUpdatePriorityIntervalDecreaseRecalculatesOffsetAndAnchor(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}

	scheduler.Register("channel-priority", p, PriorityNormal, 2*time.Hour)
	job := scheduler.jobMap["channel-priority:videos"]
	require.NotNil(t, job)

	originalOffset := job.Offset
	originalNextRunAt := job.NextRunAt
	newInterval := 30 * time.Minute
	wantOffset := calculateOffset(job.key, newInterval)

	before := time.Now()
	scheduler.UpdatePriority("channel-priority", p.Name(), PriorityHigh, newInterval)
	after := time.Now()

	require.Equal(t, PriorityHigh, job.Priority)
	require.Equal(t, newInterval, job.Interval)
	require.NotEqual(t, originalOffset, job.Offset)
	require.Equal(t, wantOffset, job.Offset)
	assert.NotEqual(t, originalNextRunAt, job.NextRunAt)

	lowerBound := nextPollAt(before, newInterval, wantOffset)
	upperBound := nextPollAt(after, newInterval, wantOffset)
	assert.False(t, job.NextRunAt.Before(lowerBound))
	assert.False(t, job.NextRunAt.After(upperBound))
}

func TestSchedulerCanRestartAfterStop(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		PollTimeout:     50 * time.Millisecond,
	})

	require.NoError(t, scheduler.RegisterChecked(
		"channel-restart",
		&togglePollerStub{name: "restart"},
		PriorityNormal,
		time.Hour,
	))

	ctx := t.Context()

	require.NotPanics(t, func() {
		scheduler.Start(ctx)
		scheduler.Stop()
		scheduler.Start(ctx)
		scheduler.Stop()
	})
}

func TestSchedulerRegisterCheckedRejectsInvalidInput(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})

	require.Error(t, scheduler.RegisterChecked("", &togglePollerStub{name: "videos"}, PriorityNormal, time.Minute))
	require.Error(t, scheduler.RegisterChecked("channel-1", nil, PriorityNormal, time.Minute))
	require.Error(t, scheduler.RegisterChecked("channel-1", &togglePollerStub{name: "videos"}, PriorityNormal, 0))
	require.Error(t, scheduler.RegisterChecked("channel-1", &togglePollerStub{name: "   "}, PriorityNormal, time.Minute))
}
