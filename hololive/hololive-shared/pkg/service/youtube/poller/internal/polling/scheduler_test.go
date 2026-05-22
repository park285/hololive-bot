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

package polling

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
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

func TestSchedulerNudgeAllJobsResetsBackoffAndWakesDispatcher(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
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

type countingPollerStub struct {
	name  string
	err   error
	calls int
}

func (p *countingPollerStub) Poll(context.Context, string) error {
	p.calls++
	return p.err
}
func (p *countingPollerStub) Name() string { return p.name }

type schedulerClaimStub struct {
	status    JobClaimStatus
	claim     *schedulerClaimHandleStub
	err       error
	tryCalls  int
	poller    string
	channelID string
}

func (c *schedulerClaimStub) TryClaim(
	_ context.Context,
	pollerName string,
	channelID string,
	_, _ time.Duration,
) (JobClaimStatus, JobClaim, error) {
	c.tryCalls++
	c.poller = pollerName
	c.channelID = channelID
	if c.claim == nil {
		return c.status, nil, c.err
	}
	return c.status, c.claim, c.err
}

type schedulerClaimHandleStub struct {
	markCompletedCalls int
	releaseCalls       int
	renewCalls         int
	renewFn            func(context.Context, time.Duration) (bool, error)
}

func (c *schedulerClaimHandleStub) Renew(ctx context.Context, ttl time.Duration) (bool, error) {
	c.renewCalls++
	if c.renewFn != nil {
		return c.renewFn(ctx, ttl)
	}
	return true, nil
}
func (c *schedulerClaimHandleStub) MarkCompleted(context.Context, time.Duration) (bool, error) {
	c.markCompletedCalls++
	return true, nil
}
func (c *schedulerClaimHandleStub) Release(context.Context) (bool, error) {
	c.releaseCalls++
	return true, nil
}

type sharedSchedulerClaimState struct {
	mu            sync.Mutex
	owner         bool
	completed     bool
	results       map[JobClaimResult]int
	markCompleted int
	releases      int
}

func newSharedSchedulerClaimState() *sharedSchedulerClaimState {
	return &sharedSchedulerClaimState{results: make(map[JobClaimResult]int)}
}

func (s *sharedSchedulerClaimState) TryClaim(
	context.Context,
	string,
	string,
	time.Duration,
	time.Duration,
) (JobClaimStatus, JobClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completed {
		s.results[JobClaimAlreadyCompleted]++
		return JobClaimStatus{Result: JobClaimAlreadyCompleted, RetryAfter: time.Minute}, nil, nil
	}
	if s.owner {
		s.results[JobClaimPeerOwned]++
		return JobClaimStatus{Result: JobClaimPeerOwned, RetryAfter: time.Minute}, nil, nil
	}
	s.owner = true
	s.results[JobClaimAcquired]++
	return JobClaimStatus{Result: JobClaimAcquired}, &sharedSchedulerClaimHandle{state: s}, nil
}

func (s *sharedSchedulerClaimState) resultCount(result JobClaimResult) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.results[result]
}

func (s *sharedSchedulerClaimState) completedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.markCompleted
}

type sharedSchedulerClaimHandle struct {
	state *sharedSchedulerClaimState
}

func (h *sharedSchedulerClaimHandle) Renew(context.Context, time.Duration) (bool, error) {
	return true, nil
}

func (h *sharedSchedulerClaimHandle) MarkCompleted(context.Context, time.Duration) (bool, error) {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	h.state.completed = true
	h.state.owner = false
	h.state.markCompleted++
	return true, nil
}

func (h *sharedSchedulerClaimHandle) Release(context.Context) (bool, error) {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	h.state.owner = false
	h.state.releases++
	return true, nil
}

type blockingCountingPollerStub struct {
	name    string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func (p *blockingCountingPollerStub) Poll(ctx context.Context, _ string) error {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	p.once.Do(func() { close(p.entered) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.release:
		return nil
	}
}

func (p *blockingCountingPollerStub) Name() string { return p.name }

func (p *blockingCountingPollerStub) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestRunJobClaimRenewLoop_StopsWhenPollContextCanceledBeforeTick(t *testing.T) {
	renewCtx, renewCancel := context.WithCancel(context.Background())
	defer renewCancel()
	pollCtx, pollCancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	claim := &schedulerClaimHandleStub{}
	done := make(chan struct{})

	pollCancel()

	go func() {
		defer close(done)
		runJobClaimRenewLoop(renewCtx, pollCtx, pollCancel, claim, "videos", time.Minute, time.Hour, errCh)
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
	renewCtx, renewCancel := context.WithCancel(context.Background())
	defer renewCancel()
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
		runJobClaimRenewLoop(renewCtx, pollCtx, pollCancel, claim, "videos", time.Minute, 5*time.Millisecond, errCh)
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
	scheduler := NewScheduler(SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 200 * time.Millisecond,
		JobClaimer: &schedulerClaimStub{
			status: JobClaimStatus{Result: JobClaimPeerOwned, RetryAfter: 25 * time.Second},
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
	scheduler := NewScheduler(SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		JobClaimer: &schedulerClaimStub{
			status: JobClaimStatus{Result: JobClaimAlreadyCompleted, RetryAfter: time.Minute},
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
	schedulerA := NewScheduler(SchedulerConfig{
		WorkerCount:     1,
		RequestInterval: 0,
		JobClaimer:      claimer,
	})
	schedulerB := NewScheduler(SchedulerConfig{
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
	require.Equal(t, 1, claimer.resultCount(JobClaimAcquired))
	require.Equal(t, 1, claimer.resultCount(JobClaimPeerOwned))
	require.Equal(t, 1, claimer.completedCount())
	require.Equal(t, 0, jobB.consecutiveFailures)
	assert.Less(t, elapsedB, 100*time.Millisecond)

	schedulerB.executeJob(context.Background(), jobB, 2)

	require.Equal(t, 1, p.callCount())
	require.Equal(t, 1, claimer.resultCount(JobClaimAlreadyCompleted))
	require.Equal(t, 0, jobB.consecutiveFailures)
}

func TestSchedulerExecuteJobFailsClosedWhenClaimUnavailable(t *testing.T) {
	claimer := &schedulerClaimStub{
		status: JobClaimStatus{Result: JobClaimUnavailable},
		err:    fmt.Errorf("valkey unavailable"),
	}
	scheduler := NewScheduler(SchedulerConfig{
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
	scheduler := NewScheduler(SchedulerConfig{
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
				status: JobClaimStatus{Result: JobClaimAcquired},
				claim:  claim,
			}
			scheduler := NewScheduler(SchedulerConfig{
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
