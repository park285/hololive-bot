package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
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

func TestRunClaimedJobPollJoinsRenewLoopBeforeFinalizingClaim(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{
		WorkerCount:            1,
		RequestInterval:        0,
		PollTimeout:            time.Second,
		ClaimCompletionTimeout: time.Second,
	})
	claim := &renewJoinClaim{}
	job := &Job{
		ChannelID: "channel-1",
		Poller:    renewJoinPoller{name: "join-test"},
		Interval:  time.Minute,
		key:       "channel-1:join-test",
		index:     -1,
	}

	pollCtx, pollCancel := context.WithCancel(context.Background())
	renewCancelObserved := make(chan struct{})
	allowRenewExit := make(chan struct{})
	renewDone := make(chan struct{})
	go func() {
		<-pollCtx.Done()
		close(renewCancelObserved)
		<-allowRenewExit
		close(renewDone)
	}()
	renew := &jobClaimRenewController{
		pollCtx: pollCtx,
		cancel:  pollCancel,
		done:    renewDone,
		active:  true,
	}

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		scheduler.runClaimedJobPoll(
			context.Background(),
			pollCtx,
			job,
			1,
			jobClaimDecision{claim: claim, claimed: true, proceed: true},
			renew,
			time.Now(),
		)
	}()

	select {
	case <-renewCancelObserved:
	case <-time.After(time.Second):
		t.Fatal("renew loop cancellation was not requested")
	}
	if got := claim.markCompletedCalls.Load(); got != 0 {
		t.Fatalf("MarkCompleted calls before renew loop exit = %d, want 0", got)
	}
	if got := claim.releaseCalls.Load(); got != 0 {
		t.Fatalf("Release calls before renew loop exit = %d, want 0", got)
	}

	close(allowRenewExit)
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("claimed poll did not finish after renew loop exited")
	}
	if got := claim.markCompletedCalls.Load(); got != 1 {
		t.Fatalf("MarkCompleted calls = %d, want 1", got)
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
		ChannelID:     "channel-1",
		Poller:        renewJoinPoller{name: "budget-join-test"},
		Interval:      time.Minute,
		key:           "channel-1:budget-join-test",
		index:         -1,
		budgetProfile: testBudgetProfile(),
	}

	pollCtx, pollCancel := context.WithCancel(context.Background())
	renewCancelObserved := make(chan struct{})
	allowRenewExit := make(chan struct{})
	renewDone := make(chan struct{})
	go func() {
		<-pollCtx.Done()
		close(renewCancelObserved)
		<-allowRenewExit
		close(renewDone)
	}()
	renew := &jobClaimRenewController{
		pollCtx: pollCtx,
		cancel:  pollCancel,
		done:    renewDone,
		active:  true,
	}

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		scheduler.runClaimedJobPoll(
			context.Background(),
			pollCtx,
			job,
			1,
			jobClaimDecision{claim: claim, claimed: true, proceed: true},
			renew,
			time.Now(),
		)
	}()

	select {
	case <-renewCancelObserved:
	case <-time.After(time.Second):
		t.Fatal("renew loop cancellation was not requested before budget skip")
	}
	if got := claim.releaseCalls.Load(); got != 0 {
		t.Fatalf("Release calls before renew loop exit = %d, want 0", got)
	}

	close(allowRenewExit)
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("budget-skipped poll did not finish after renew loop exited")
	}
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
		ChannelID: "channel-1",
		Poller:    renewJoinPoller{name: "join-timeout-test"},
		Interval:  time.Minute,
		key:       "channel-1:join-timeout-test",
		index:     -1,
	}

	pollCtx, pollCancel := context.WithCancel(context.Background())
	renewDone := make(chan struct{})
	renew := &jobClaimRenewController{
		pollCtx: pollCtx,
		cancel:  pollCancel,
		done:    renewDone,
		active:  true,
	}

	scheduler.runClaimedJobPoll(
		context.Background(),
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
}

func TestJobClaimRenewControllerWaitDetachesCanceledParent(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	done := make(chan struct{})
	controller := &jobClaimRenewController{
		pollCtx: context.Background(),
		cancel:  func() {},
		done:    done,
		active:  true,
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(done)
	}()

	if err := controller.StopAndWait(parent, time.Second); err != nil {
		t.Fatalf("StopAndWait() error = %v, want nil with detached cleanup context", err)
	}
}
