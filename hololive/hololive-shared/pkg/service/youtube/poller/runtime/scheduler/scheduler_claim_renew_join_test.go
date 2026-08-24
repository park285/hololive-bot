package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
)

type renewJoinPoller struct {
	name string
}

func (p renewJoinPoller) Poll(context.Context, string) error { return nil }
func (p renewJoinPoller) Name() string                       { return p.name }

type renewJoinClaim struct {
	markCompletedCalls atomic.Int32
	releaseCalls       atomic.Int32
}

func (c *renewJoinClaim) Renew(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (c *renewJoinClaim) MarkCompleted(context.Context, time.Duration) (bool, error) {
	c.markCompletedCalls.Add(1)

	return true, nil
}

func (c *renewJoinClaim) Release(context.Context) (bool, error) {
	c.releaseCalls.Add(1)

	return true, nil
}

type renewJoinHarness struct {
	renew          *jobClaimRenewController
	cancelObserved <-chan struct{}
	allowExit      chan struct{}
}

func newRenewJoinHarness(t *testing.T) (*renewJoinHarness, context.Context) {
	t.Helper()

	pollCtx, pollCancel := context.WithCancel(t.Context())
	cancelObserved := make(chan struct{})
	allowExit := make(chan struct{})
	renewDone := make(chan struct{})

	go func() {
		<-pollCtx.Done()
		close(cancelObserved)
		<-allowExit
		close(renewDone)
	}()

	return &renewJoinHarness{
		renew:          &jobClaimRenewController{cancel: pollCancel, done: renewDone, active: true},
		cancelObserved: cancelObserved,
		allowExit:      allowExit,
	}, pollCtx
}

func startClaimedJobPollAsync(
	ctx context.Context,
	pollCtx context.Context,
	scheduler *Scheduler,
	harness *renewJoinHarness,
	job *Job,
	claim polling.JobClaim,
) <-chan struct{} {
	runDone := make(chan struct{})

	go func() {
		defer close(runDone)

		scheduler.runClaimedJobPoll(
			ctx,
			pollCtx,
			job,
			1,
			jobClaimDecision{claim: claim, claimed: true, proceed: true},
			harness.renew,
			time.Now(),
		)
	}()

	return runDone
}

const testChannelWaitTimeout = time.Second

func requireClosedWithin(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(testChannelWaitTimeout):
		t.Fatal(msg)
	}
}

func TestRunClaimedJobPollJoinsRenewLoopBeforeFinalizingClaim(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:            1,
		RequestInterval:        0,
		PollTimeout:            time.Second,
		ClaimCompletionTimeout: time.Second,
	})
	claim := &renewJoinClaim{}
	job := &Job{
		ChannelID: testChannelID,
		Poller:    renewJoinPoller{name: "join-test"},
		Interval:  time.Minute,
		key:       "channel-1:join-test",
		index:     -1,
	}

	harness, pollCtx := newRenewJoinHarness(t)
	runDone := startClaimedJobPollAsync(t.Context(), pollCtx, scheduler, harness, job, claim)

	requireClosedWithin(t, harness.cancelObserved, "renew loop cancellation was not requested")

	if got := claim.markCompletedCalls.Load(); got != 0 {
		t.Fatalf("MarkCompleted calls before renew loop exit = %d, want 0", got)
	}

	if got := claim.releaseCalls.Load(); got != 0 {
		t.Fatalf("Release calls before renew loop exit = %d, want 0", got)
	}

	close(harness.allowExit)
	requireClosedWithin(t, runDone, "claimed poll did not finish after renew loop exited")

	if got := claim.markCompletedCalls.Load(); got != 1 {
		t.Fatalf("MarkCompleted calls = %d, want 1", got)
	}

	if got := testutil.ToFloat64(scheduler.metrics.PollerLastSuccessTimestamp.WithLabelValues(job.Poller.Name())); got <= 0 {
		t.Fatalf("poller last success timestamp = %v, want positive Unix timestamp", got)
	}
}

func TestBudgetSkipJoinsRenewLoopBeforeReleasingClaim(t *testing.T) {
	limiter := &schedulerBudgetLimiterStub{decision: polling.BudgetDecision{
		Allowed:    false,
		RetryAfter: time.Minute,
	}}
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:            1,
		RequestInterval:        0,
		PollTimeout:            time.Second,
		ClaimCompletionTimeout: time.Second,
		BudgetLimiter:          limiter,
		BudgetContext:          polling.BudgetContext{Enabled: true},
	})
	claim := &renewJoinClaim{}
	job := &Job{
		ChannelID:     testChannelID,
		Poller:        renewJoinPoller{name: "budget-join-test"},
		Interval:      time.Minute,
		key:           "channel-1:budget-join-test",
		index:         -1,
		budgetProfile: testBudgetProfile(),
	}

	harness, pollCtx := newRenewJoinHarness(t)
	runDone := startClaimedJobPollAsync(t.Context(), pollCtx, scheduler, harness, job, claim)

	requireClosedWithin(t, harness.cancelObserved, "renew loop cancellation was not requested before budget skip")

	if got := claim.releaseCalls.Load(); got != 0 {
		t.Fatalf("Release calls before renew loop exit = %d, want 0", got)
	}

	close(harness.allowExit)
	requireClosedWithin(t, runDone, "budget-skipped poll did not finish after renew loop exited")

	if got := claim.releaseCalls.Load(); got != 1 {
		t.Fatalf("Release calls after renew loop exit = %d, want 1", got)
	}

	if got := claim.markCompletedCalls.Load(); got != 0 {
		t.Fatalf("MarkCompleted calls for budget skip = %d, want 0", got)
	}
}

func TestRunClaimedJobPollLeavesClaimForTTLWhenRenewJoinTimesOut(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:            1,
		RequestInterval:        0,
		PollTimeout:            time.Second,
		ClaimCompletionTimeout: 20 * time.Millisecond,
	})
	claim := &renewJoinClaim{}
	job := &Job{
		ChannelID: testChannelID,
		Poller:    renewJoinPoller{name: "join-timeout-test"},
		Interval:  time.Minute,
		key:       "channel-1:join-timeout-test",
		index:     -1,
	}

	pollCtx, pollCancel := context.WithCancel(t.Context())
	renewDone := make(chan struct{})
	renew := &jobClaimRenewController{
		cancel: pollCancel,
		done:   renewDone,
		active: true,
	}

	scheduler.runClaimedJobPoll(
		t.Context(),
		pollCtx,
		job,
		1,
		jobClaimDecision{claim: claim, claimed: true, proceed: true},
		renew,
		time.Now(),
	)
	close(renewDone)

	if got := claim.markCompletedCalls.Load(); got != 0 {
		t.Fatalf("MarkCompleted calls after renew join timeout = %d, want 0", got)
	}

	if got := claim.releaseCalls.Load(); got != 0 {
		t.Fatalf("Release calls after renew join timeout = %d, want 0 (TTL fail-closed)", got)
	}

	if got := testutil.ToFloat64(scheduler.metrics.PollerLastSuccessTimestamp.WithLabelValues(job.Poller.Name())); got != 0 {
		t.Fatalf("poller last success timestamp after renew join timeout = %v, want 0", got)
	}
}

func TestJobClaimRenewControllerWaitDetachesCanceledParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(t.Context())
	cancelParent()

	done := make(chan struct{})
	controller := &jobClaimRenewController{
		cancel: func() {},
		done:   done,
		active: true,
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(done)
	}()

	if err := controller.StopAndWait(parent, time.Second); err != nil {
		t.Fatalf("StopAndWait() error = %v, want nil with detached cleanup context", err)
	}
}
