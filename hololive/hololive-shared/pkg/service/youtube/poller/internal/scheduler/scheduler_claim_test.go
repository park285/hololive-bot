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
	"fmt"
	"log/slog"
	"testing"
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunJobClaimRenewLoop_StopsWhenPollContextCanceledBeforeTick(t *testing.T) {
	renewCtx := t.Context()
	pollCtx, pollCancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	claim := &schedulerClaimHandleStub{}
	done := make(chan struct{})

	pollCancel()

	go func() {
		defer close(done)
		runJobClaimRenewLoop(renewCtx, pollCtx, pollCancel, claim, "videos", time.Minute, time.Hour, errCh, testMetrics, slog.Default())
	}()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runJobClaimRenewLoop did not stop after poll context cancellation")
	}

	require.Equal(t, 0, claim.renewCalls)
	select {
	case err := <-errCh:
		t.Fatalf("unexpected renew error: %v", err)
	default:
	}
}

func TestRunJobClaimRenewLoop_RenewFailureCancelsPollAndReportsError(t *testing.T) {
	renewCtx := t.Context()
	pollCtx, pollCancel := context.WithCancel(context.Background())
	defer pollCancel()
	errCh := make(chan error, 1)
	claim := &schedulerClaimHandleStub{
		renewFn: func(context.Context, time.Duration) (bool, error) {
			return false, nil
		},
	}
	done := make(chan struct{})

	go func() {
		defer close(done)
		runJobClaimRenewLoop(renewCtx, pollCtx, pollCancel, claim, "videos", time.Minute, 5*time.Millisecond, errCh, testMetrics, slog.Default())
	}()

	select {
	case err := <-errCh:
		require.ErrorContains(t, err, "ownership lost")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runJobClaimRenewLoop did not report renew failure")
	}

	select {
	case <-pollCtx.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runJobClaimRenewLoop did not cancel poll context after renew failure")
	}

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runJobClaimRenewLoop did not stop after renew failure")
	}

	require.Equal(t, 1, claim.renewCalls)
}

func TestSchedulerExecuteJobSkipsPeerOwnedWithoutPolling(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 200 * time.Millisecond,
		JobClaimer: &schedulerClaimStub{
			status: polling.JobClaimStatus{Result: polling.JobClaimPeerOwned, RetryAfter: 25 * time.Second},
		},
	})
	require.NoError(t, scheduler.rateLimiter.Wait(context.Background()))
	p := &countingPollerStub{name: "videos"}
	scheduler.Register("channel-peer", p, PriorityNormal, time.Hour)
	job := scheduler.jobMap["channel-peer:videos"]
	require.NotNil(t, job)
	heap.Remove(&scheduler.jobs, job.index)

	before := time.Now()
	scheduler.executeJob(context.Background(), job, 1)
	after := time.Now()

	require.Equal(t, 0, p.calls)
	require.Equal(t, 0, job.consecutiveFailures)
	require.Less(t, time.Since(before), 100*time.Millisecond)
	assert.False(t, job.NextRunAt.Before(before.Add(25*time.Second)))
	assert.False(t, job.NextRunAt.After(after.Add(25*time.Second+100*time.Millisecond)))
}

func TestSchedulerExecuteJobSkipsAlreadyCompletedWithoutPolling(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		JobClaimer: &schedulerClaimStub{
			status: polling.JobClaimStatus{Result: polling.JobClaimAlreadyCompleted, RetryAfter: time.Minute},
		},
	})
	p := &countingPollerStub{name: "community"}
	scheduler.Register("channel-done", p, PriorityNormal, time.Hour)
	job := scheduler.jobMap["channel-done:community"]
	require.NotNil(t, job)
	heap.Remove(&scheduler.jobs, job.index)

	scheduler.executeJob(context.Background(), job, 1)

	require.Equal(t, 0, p.calls)
	require.Equal(t, 0, job.consecutiveFailures)
}

func TestSchedulerActiveActiveSharedClaimerAllowsOnlyOnePoll(t *testing.T) {
	claimer := newSharedSchedulerClaimState()
	p := &blockingCountingPollerStub{
		name:    "videos",
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	schedulerA := NewScheduler(&SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		JobClaimer:      claimer,
	})
	schedulerB := NewScheduler(&SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 200 * time.Millisecond,
		JobClaimer:      claimer,
	})
	require.NoError(t, schedulerB.rateLimiter.Wait(context.Background()))
	schedulerA.Register("channel-shared", p, PriorityNormal, time.Minute)
	schedulerB.Register("channel-shared", p, PriorityNormal, time.Minute)
	jobA := schedulerA.jobMap["channel-shared:videos"]
	jobB := schedulerB.jobMap["channel-shared:videos"]
	require.NotNil(t, jobA)
	require.NotNil(t, jobB)
	heap.Remove(&schedulerA.jobs, jobA.index)
	heap.Remove(&schedulerB.jobs, jobB.index)

	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		schedulerA.executeJob(context.Background(), jobA, 1)
	}()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("scheduler A did not start polling")
	}

	doneB := make(chan struct{})
	startB := time.Now()
	go func() {
		defer close(doneB)
		schedulerB.executeJob(context.Background(), jobB, 2)
	}()
	select {
	case <-doneB:
	case <-time.After(time.Second):
		t.Fatal("scheduler B did not skip peer-owned claim")
	}
	elapsedB := time.Since(startB)
	close(p.release)
	select {
	case <-doneA:
	case <-time.After(time.Second):
		t.Fatal("scheduler A did not finish polling")
	}

	require.Equal(t, 1, p.callCount())
	require.Equal(t, 1, claimer.resultCount(polling.JobClaimAcquired))
	require.Equal(t, 1, claimer.resultCount(polling.JobClaimPeerOwned))
	require.Equal(t, 1, claimer.completedCount())
	require.Equal(t, 0, jobB.consecutiveFailures)
	assert.Less(t, elapsedB, 100*time.Millisecond)

	schedulerB.executeJob(context.Background(), jobB, 2)

	require.Equal(t, 1, p.callCount())
	require.Equal(t, 1, claimer.resultCount(polling.JobClaimAlreadyCompleted))
	require.Equal(t, 0, jobB.consecutiveFailures)
}

func TestSchedulerExecuteJobFailsClosedWhenClaimUnavailable(t *testing.T) {
	claimer := &schedulerClaimStub{
		status: polling.JobClaimStatus{Result: polling.JobClaimUnavailable},
		err:    fmt.Errorf("valkey unavailable"),
	}
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		JobClaimer:      claimer,
		ErrorBackoffMin: time.Second,
		ErrorBackoffMax: time.Second,
	})
	p := &countingPollerStub{name: "shorts"}
	scheduler.Register("channel-unavailable", p, PriorityNormal, time.Hour)
	job := scheduler.jobMap["channel-unavailable:shorts"]
	require.NotNil(t, job)
	heap.Remove(&scheduler.jobs, job.index)

	scheduler.executeJob(context.Background(), job, 1)

	require.Equal(t, 1, claimer.tryCalls)
	require.Equal(t, 0, p.calls)
	require.Equal(t, 1, job.consecutiveFailures)
}

func TestSchedulerExecuteJobWithoutClaimerKeepsPolling(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
	})
	p := &countingPollerStub{name: "videos"}
	scheduler.Register("channel-legacy", p, PriorityNormal, time.Hour)
	job := scheduler.jobMap["channel-legacy:videos"]
	require.NotNil(t, job)
	heap.Remove(&scheduler.jobs, job.index)

	scheduler.executeJob(context.Background(), job, 1)

	require.Equal(t, 1, p.calls)
	require.Equal(t, 0, job.consecutiveFailures)
}

func TestSchedulerExecuteJobCompletesOrReleasesClaim(t *testing.T) {
	tests := map[string]struct {
		pollErr       error
		wantCompleted int
		wantReleased  int
	}{
		"success marks completed": {
			wantCompleted: 1,
		},
		"failure releases": {
			pollErr:      assert.AnError,
			wantReleased: 1,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			claim := &schedulerClaimHandleStub{}
			claimer := &schedulerClaimStub{
				status: polling.JobClaimStatus{Result: polling.JobClaimAcquired},
				claim:  claim,
			}
			scheduler := NewScheduler(&SchedulerConfig{
				WorkerCount:     1,
				RequestInterval: 0,
				JobClaimer:      claimer,
			})
			p := &countingPollerStub{name: "videos", err: tc.pollErr}
			scheduler.Register("channel-claim", p, PriorityNormal, time.Hour)
			job := scheduler.jobMap["channel-claim:videos"]
			require.NotNil(t, job)
			heap.Remove(&scheduler.jobs, job.index)

			scheduler.executeJob(context.Background(), job, 1)

			require.Equal(t, 1, p.calls)
			require.Equal(t, 1, claimer.tryCalls)
			require.Equal(t, "videos", claimer.poller)
			require.Equal(t, "channel-claim", claimer.channelID)
			require.Equal(t, tc.wantCompleted, claim.markCompletedCalls)
			require.Equal(t, tc.wantReleased, claim.releaseCalls)
		})
	}
}

func TestSchedulerJobClaimLeaseTTLIncludesBudgetAndCompletionWindows(t *testing.T) {
	defaults := DefaultSchedulerConfig()
	require.Equal(t, 3*time.Second, defaults.BudgetAcquireTimeout)
	require.Equal(t, 5*time.Second, defaults.ClaimCompletionTimeout)
	require.Equal(t, 15*time.Second, defaults.ClaimLeaseSafetyMargin)

	minScheduler := NewScheduler(&SchedulerConfig{
		PollTimeout:            2 * time.Second,
		BudgetAcquireTimeout:   3 * time.Second,
		ClaimCompletionTimeout: 4 * time.Second,
		ClaimLeaseSafetyMargin: 5 * time.Second,
	})
	require.Equal(t, time.Minute, minScheduler.jobClaimLeaseTTL())

	base := NewScheduler(&SchedulerConfig{
		PollTimeout:            2 * time.Minute,
		BudgetAcquireTimeout:   3 * time.Second,
		ClaimCompletionTimeout: 4 * time.Second,
		ClaimLeaseSafetyMargin: 5 * time.Second,
	})
	require.Equal(t, 2*time.Minute+12*time.Second, base.jobClaimLeaseTTL())

	corrected := NewScheduler(&SchedulerConfig{
		PollTimeout:            2 * time.Minute,
		BudgetAcquireTimeout:   0,
		ClaimCompletionTimeout: -time.Second,
		ClaimLeaseSafetyMargin: 0,
	})
	require.Equal(t, 2*time.Minute+23*time.Second, corrected.jobClaimLeaseTTL())

	increasedBudget := NewScheduler(&SchedulerConfig{
		PollTimeout:            2 * time.Minute,
		BudgetAcquireTimeout:   20 * time.Second,
		ClaimCompletionTimeout: 4 * time.Second,
		ClaimLeaseSafetyMargin: 5 * time.Second,
	})
	require.Greater(t, increasedBudget.jobClaimLeaseTTL(), base.jobClaimLeaseTTL())

	noClamp := NewScheduler(&SchedulerConfig{
		PollTimeout:            90 * time.Minute,
		BudgetAcquireTimeout:   time.Minute,
		ClaimCompletionTimeout: time.Minute,
		ClaimLeaseSafetyMargin: time.Minute,
	})
	require.Equal(t, 93*time.Minute, noClamp.jobClaimLeaseTTL())
}

func TestSchedulerFinishJobClaimDetachesCompletionFromCanceledParentContext(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
	})
	claim := &schedulerClaimHandleStub{}
	job := &Job{
		ChannelID: "channel-claim",
		Poller:    &togglePollerStub{name: "videos"},
		Interval:  time.Minute,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := scheduler.finishJobClaim(ctx, job, claim, nil)

	require.NoError(t, err)
	require.Equal(t, 1, claim.markCompletedCalls)
	require.NoError(t, claim.markCompletedCtxErr)
}
