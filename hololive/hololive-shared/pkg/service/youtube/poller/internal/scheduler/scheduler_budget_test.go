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
	"testing"
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulerExecuteJobBudgetAllowedPollsAndCommits(t *testing.T) {
	reservation := &schedulerBudgetReservationStub{}
	limiter := &schedulerBudgetLimiterStub{
		decision:    polling.BudgetDecision{Allowed: true},
		reservation: reservation,
	}
	claim := &schedulerClaimHandleStub{}
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:            1,
		RequestInterval:        0,
		PollTimeout:            2 * time.Second,
		JobClaimer:             &schedulerClaimStub{status: polling.JobClaimStatus{Result: polling.JobClaimAcquired}, claim: claim},
		BudgetLimiter:          limiter,
		BudgetContext:          polling.BudgetContext{Namespace: "test", InstanceID: "worker-a", Enabled: true},
		BudgetAcquireTimeout:   time.Second,
		ClaimCompletionTimeout: time.Second,
		ClaimLeaseSafetyMargin: time.Second,
	})
	profile := testBudgetProfile()
	p := &countingPollerStub{name: "videos"}
	require.NoError(t, scheduler.RegisterCheckedWithBudgetProfile("channel-budget", p, PriorityHigh, time.Hour, profile))
	job := scheduler.jobMap["channel-budget:videos"]
	require.NotNil(t, job)
	heap.Remove(&scheduler.jobs, job.index)

	scheduler.executeJob(context.Background(), job, 1)

	require.Equal(t, 1, p.calls)
	require.Equal(t, 1, limiter.callCount())
	require.Equal(t, polling.BudgetJob{
		Namespace:  "test",
		InstanceID: "worker-a",
		PollerName: "videos",
		ChannelID:  "channel-budget",
		JobKey:     "channel-budget:videos",
	}, limiter.job)
	require.Equal(t, profile, limiter.profile)
	require.Equal(t, scheduler.jobClaimLeaseTTL(), limiter.ttl)
	require.NoError(t, limiter.ctxErr)
	require.Equal(t, 1, claim.markCompletedCalls)
	require.Equal(t, 0, claim.releaseCalls)
	require.Equal(t, 1, reservation.commitCalls)
	require.Equal(t, 0, reservation.releaseCalls)
	require.NoError(t, reservation.commitCtxErr)
}

func TestSchedulerExecuteJobBudgetDeniedSkipsPollAndUsesRetryAfter(t *testing.T) {
	retryAfter := 17 * time.Second
	claim := &schedulerClaimHandleStub{}
	limiter := &schedulerBudgetLimiterStub{
		decision: polling.BudgetDecision{Allowed: false, RetryAfter: retryAfter, Reason: string(JobSkipBudgetExhausted)},
	}
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:          1,
		RequestInterval:      0,
		JobClaimer:           &schedulerClaimStub{status: polling.JobClaimStatus{Result: polling.JobClaimAcquired}, claim: claim},
		BudgetLimiter:        limiter,
		BudgetContext:        polling.BudgetContext{Namespace: "test", InstanceID: "worker-a", Enabled: true},
		BudgetAcquireTimeout: time.Second,
		ErrorBackoffMin:      5 * time.Second,
		ErrorBackoffMax:      5 * time.Second,
	})
	p := &countingPollerStub{name: "videos"}
	require.NoError(t, scheduler.RegisterCheckedWithBudgetProfile("channel-denied", p, PriorityHigh, time.Hour, testBudgetProfile()))
	job := scheduler.jobMap["channel-denied:videos"]
	require.NotNil(t, job)
	heap.Remove(&scheduler.jobs, job.index)

	before := time.Now()
	scheduler.executeJob(context.Background(), job, 1)
	after := time.Now()

	require.Equal(t, 0, p.calls)
	require.Equal(t, 1, limiter.callCount())
	require.Equal(t, 0, claim.markCompletedCalls)
	require.Equal(t, 1, claim.releaseCalls)
	require.NoError(t, claim.releaseCtxErr)
	require.Equal(t, 0, job.consecutiveFailures)
	assert.False(t, job.NextRunAt.Before(before.Add(retryAfter)))
	assert.False(t, job.NextRunAt.After(after.Add(retryAfter+100*time.Millisecond)))
}

func TestSchedulerExecuteJobBudgetLimiterErrorReleasesClaimAndBacksOff(t *testing.T) {
	claim := &schedulerClaimHandleStub{}
	limiter := &schedulerBudgetLimiterStub{err: assert.AnError}
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:          1,
		RequestInterval:      0,
		JobClaimer:           &schedulerClaimStub{status: polling.JobClaimStatus{Result: polling.JobClaimAcquired}, claim: claim},
		BudgetLimiter:        limiter,
		BudgetContext:        polling.BudgetContext{Namespace: "test", InstanceID: "worker-a", Enabled: true},
		BudgetAcquireTimeout: time.Second,
		ErrorBackoffMin:      11 * time.Second,
		ErrorBackoffMax:      11 * time.Second,
	})
	p := &countingPollerStub{name: "videos"}
	require.NoError(t, scheduler.RegisterCheckedWithBudgetProfile("channel-budget-error", p, PriorityHigh, time.Hour, testBudgetProfile()))
	job := scheduler.jobMap["channel-budget-error:videos"]
	require.NotNil(t, job)
	heap.Remove(&scheduler.jobs, job.index)

	before := time.Now()
	scheduler.executeJob(context.Background(), job, 1)
	after := time.Now()

	require.Equal(t, 0, p.calls)
	require.Equal(t, 1, limiter.callCount())
	require.Equal(t, 0, claim.markCompletedCalls)
	require.Equal(t, 1, claim.releaseCalls)
	require.Equal(t, 1, job.consecutiveFailures)
	assert.False(t, job.NextRunAt.Before(before.Add(11*time.Second)))
	assert.False(t, job.NextRunAt.After(after.Add(11*time.Second+100*time.Millisecond)))
}

func TestBudgetLimiterDisabledOrEmptyProfileSkipsReserve(t *testing.T) {
	tests := map[string]struct {
		limiter       *schedulerBudgetLimiterStub
		budgetContext polling.BudgetContext
		profile       polling.BudgetProfile
	}{
		"nil limiter": {
			profile: testBudgetProfile(),
		},
		"disabled context": {
			limiter:       &schedulerBudgetLimiterStub{decision: polling.BudgetDecision{Allowed: true}},
			budgetContext: polling.BudgetContext{Namespace: "test", InstanceID: "worker-a", Enabled: false},
			profile:       testBudgetProfile(),
		},
		"empty source units": {
			limiter:       &schedulerBudgetLimiterStub{decision: polling.BudgetDecision{Allowed: true}},
			budgetContext: polling.BudgetContext{Namespace: "test", InstanceID: "worker-a", Enabled: true},
			profile:       polling.BudgetProfile{SourceUnits: map[polling.BudgetSource]float64{}, BurstClass: polling.BudgetBurstPrimary, Priority: polling.BudgetPriorityHigh},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			scheduler := NewScheduler(&SchedulerConfig{
				WorkerCount:          1,
				RequestInterval:      0,
				BudgetLimiter:        tc.limiter,
				BudgetContext:        tc.budgetContext,
				BudgetAcquireTimeout: time.Second,
			})
			p := &countingPollerStub{name: "videos"}
			require.NoError(t, scheduler.RegisterCheckedWithBudgetProfile("channel-disabled", p, PriorityHigh, time.Hour, tc.profile))
			job := scheduler.jobMap["channel-disabled:videos"]
			require.NotNil(t, job)
			heap.Remove(&scheduler.jobs, job.index)

			scheduler.executeJob(context.Background(), job, 1)

			require.Equal(t, 1, p.calls)
			require.Equal(t, 0, job.consecutiveFailures)
			if tc.limiter != nil {
				require.Equal(t, 0, tc.limiter.callCount())
			}
		})
	}
}

func TestSchedulerExecuteJobPollFailureReleasesBudgetReservation(t *testing.T) {
	reservation := &schedulerBudgetReservationStub{}
	claim := &schedulerClaimHandleStub{}
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:          1,
		RequestInterval:      0,
		JobClaimer:           &schedulerClaimStub{status: polling.JobClaimStatus{Result: polling.JobClaimAcquired}, claim: claim},
		BudgetLimiter:        &schedulerBudgetLimiterStub{decision: polling.BudgetDecision{Allowed: true}, reservation: reservation},
		BudgetContext:        polling.BudgetContext{Namespace: "test", InstanceID: "worker-a", Enabled: true},
		BudgetAcquireTimeout: time.Second,
		ErrorBackoffMin:      7 * time.Second,
		ErrorBackoffMax:      7 * time.Second,
	})
	p := &countingPollerStub{name: "videos", err: assert.AnError}
	require.NoError(t, scheduler.RegisterCheckedWithBudgetProfile("channel-poll-fail", p, PriorityHigh, time.Hour, testBudgetProfile()))
	job := scheduler.jobMap["channel-poll-fail:videos"]
	require.NotNil(t, job)
	heap.Remove(&scheduler.jobs, job.index)

	scheduler.executeJob(context.Background(), job, 1)

	require.Equal(t, 1, p.calls)
	require.Equal(t, 1, claim.releaseCalls)
	require.Equal(t, 0, claim.markCompletedCalls)
	require.Equal(t, 0, reservation.commitCalls)
	require.Equal(t, 1, reservation.releaseCalls)
	require.NoError(t, reservation.releaseCtxErr)
}
