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

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_SetProxyEnabled(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{RequestInterval: 0})
	poller := &togglePollerStub{name: "toggle"}

	scheduler.Register("channel-1", poller, PriorityNormal, time.Minute)
	scheduler.Register("channel-2", poller, PriorityNormal, time.Minute) // 동일 poller 중복 등록

	applied := scheduler.SetProxyEnabled(true)
	assert.Equal(t, 1, applied)

	enabled, known := scheduler.ProxyEnabled()
	assert.True(t, known)
	assert.True(t, enabled)
}

func TestSchedulerRegisterSignalsWakeChannel(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})

	scheduler.Register("channel-1", &togglePollerStub{name: "videos"}, PriorityNormal, time.Minute)

	select {
	case <-scheduler.wakeCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected scheduler register to signal wakeCh")
	}
}

func TestSchedulerNudgeAllJobsResetsBackoffAndWakesDispatcher(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	poller := &togglePollerStub{name: "videos"}

	require.NoError(t, scheduler.RegisterChecked("channel-1", poller, PriorityNormal, time.Minute))
	require.NoError(t, scheduler.RegisterChecked("channel-2", poller, PriorityNormal, time.Minute))

	future := time.Now().Add(10 * time.Minute)
	scheduler.mu.Lock()
	for _, job := range scheduler.jobMap {
		job.consecutiveFailures = 5
		job.NextRunAt = future
	}
	select {
	case <-scheduler.wakeCh:
	default:
	}
	scheduler.mu.Unlock()

	scheduler.NudgeAllJobs()

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	for _, job := range scheduler.jobMap {
		assert.Equal(t, 0, job.consecutiveFailures, "consecutive_failures must reset")
		assert.False(t, job.NextRunAt.After(time.Now().Add(time.Second)), "NextRunAt must be due immediately")
	}
	select {
	case <-scheduler.wakeCh:
	default:
		t.Fatal("dispatcher wake channel was not signalled")
	}
}

func TestSchedulerSyncPollerTargetsAddsAndRemovesJobs(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-old", p, PriorityNormal, time.Minute)

	scheduler.SyncPollerTargets(&PollerTargetSync{
		Poller:     p,
		Priority:   PriorityHigh,
		Interval:   2 * time.Minute,
		ChannelIDs: []string{"channel-new"},
	})

	require.NotContains(t, scheduler.jobMap, "channel-old:videos")
	require.Contains(t, scheduler.jobMap, "channel-new:videos")
	newJob, ok := scheduler.jobMap["channel-new:videos"]
	require.True(t, ok)
	require.NotNil(t, newJob)
	assert.Equal(t, PriorityHigh, newJob.Priority)
	assert.Equal(t, 2*time.Minute, newJob.Interval)
}

func TestSchedulerSyncPollerTargetsRetiresInflightJobWithoutRequeue(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-old", p, PriorityNormal, time.Minute)

	job := scheduler.jobMap["channel-old:videos"]
	require.NotNil(t, job)
	heap.Pop(&scheduler.jobs)
	require.Equal(t, -1, job.index)

	scheduler.SyncPollerTargets(&PollerTargetSync{
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
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	scheduler.Register("channel-existing", p, PriorityNormal, time.Hour)

	existing := scheduler.jobMap["channel-existing:videos"]
	require.NotNil(t, existing)
	existingNextRunAt := existing.NextRunAt

	before := time.Now()
	scheduler.SyncPollerTargets(&PollerTargetSync{
		Poller:                 p,
		Priority:               PriorityHigh,
		Interval:               time.Hour,
		ChannelIDs:             []string{"channel-existing", "channel-new"},
		ForceImmediateFirstRun: true,
	})
	after := time.Now()

	require.Contains(t, scheduler.jobMap, "channel-new:videos")
	existingJob, ok := scheduler.jobMap["channel-existing:videos"]
	require.True(t, ok)
	require.NotNil(t, existingJob)
	assert.Equal(t, existingNextRunAt, existingJob.NextRunAt)

	newJob, ok := scheduler.jobMap["channel-new:videos"]
	require.True(t, ok)
	require.NotNil(t, newJob)
	assert.False(t, newJob.NextRunAt.Before(before))
	assert.False(t, newJob.NextRunAt.After(after))
}

func TestSchedulerRegisterAndSyncPropagateBudgetProfile(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	p := &togglePollerStub{name: "videos"}
	profile := testBudgetProfile()

	require.NoError(t, scheduler.RegisterCheckedWithBudgetProfile("channel-direct", p, PriorityHigh, time.Hour, profile))

	directJob, ok := scheduler.jobMap["channel-direct:videos"]
	require.True(t, ok)
	require.NotNil(t, directJob)
	require.Equal(t, profile, directJob.budgetProfile)

	syncProfile := polling.BudgetProfile{
		SourceUnits: map[polling.BudgetSource]float64{polling.BudgetSourceHolodexLive: 2},
		BurstClass:  polling.BudgetBurstBackfill,
		Priority:    polling.BudgetPriorityLow,
	}
	scheduler.SyncPollerTargets(&PollerTargetSync{
		Poller:        p,
		Priority:      PriorityLow,
		Interval:      2 * time.Hour,
		ChannelIDs:    []string{"channel-sync"},
		BudgetProfile: syncProfile,
	})
	syncJob, ok := scheduler.jobMap["channel-sync:videos"]
	require.True(t, ok)
	require.NotNil(t, syncJob)
	require.Equal(t, syncProfile, syncJob.budgetProfile)

	updatedProfile := polling.BudgetProfile{
		SourceUnits: map[polling.BudgetSource]float64{polling.BudgetSourceBrowserSnapshot: 3},
		BurstClass:  polling.BudgetBurstFallback,
		Priority:    polling.BudgetPriorityNormal,
	}
	scheduler.SyncPollerTargets(&PollerTargetSync{
		Poller:        p,
		Priority:      PriorityNormal,
		Interval:      3 * time.Hour,
		ChannelIDs:    []string{"channel-sync"},
		BudgetProfile: updatedProfile,
	})
	updatedJob, ok := scheduler.jobMap["channel-sync:videos"]
	require.True(t, ok)
	require.NotNil(t, updatedJob)
	require.Equal(t, updatedProfile, updatedJob.budgetProfile)
}

func TestSchedulerCanRestartAfterStop(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
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
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})

	require.Error(t, scheduler.RegisterChecked("", &togglePollerStub{name: "videos"}, PriorityNormal, time.Minute))
	require.Error(t, scheduler.RegisterChecked("channel-1", nil, PriorityNormal, time.Minute))
	require.Error(t, scheduler.RegisterChecked("channel-1", &togglePollerStub{name: "videos"}, PriorityNormal, 0))
	require.Error(t, scheduler.RegisterChecked("channel-1", &togglePollerStub{name: "   "}, PriorityNormal, time.Minute))
}
