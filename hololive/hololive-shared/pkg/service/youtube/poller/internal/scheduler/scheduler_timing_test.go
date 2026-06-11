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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextPollAt_KeepsAnchor(t *testing.T) {
	now := time.Date(2026, time.April, 9, 10, 2, 10, 0, time.UTC)
	interval := 5 * time.Minute
	offset := 2 * time.Minute

	got := nextPollAt(now, interval, offset)
	want := time.Date(2026, time.April, 9, 10, 7, 0, 0, time.UTC)

	assert.Equal(t, want, got)
}

func TestAdvanceNextRunAt_PreservesAnchorAcrossBacklog(t *testing.T) {
	scheduledAt := time.Date(2026, time.April, 9, 10, 7, 0, 0, time.UTC)
	now := time.Date(2026, time.April, 9, 10, 17, 30, 0, time.UTC)

	got := advanceNextRunAt(scheduledAt, 5*time.Minute, now)
	want := time.Date(2026, time.April, 9, 10, 22, 0, 0, time.UTC)

	assert.Equal(t, want, got)
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
