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
	"sync"
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
)

var testMetrics = polling.NewMetrics()

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
	status    polling.JobClaimStatus
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
) (polling.JobClaimStatus, polling.JobClaim, error) {
	c.tryCalls++
	c.poller = pollerName
	c.channelID = channelID
	if c.claim == nil {
		return c.status, nil, c.err
	}
	return c.status, c.claim, c.err
}

type schedulerClaimHandleStub struct {
	markCompletedCalls  int
	releaseCalls        int
	renewCalls          int
	markCompletedCtxErr error
	releaseCtxErr       error
	renewFn             func(context.Context, time.Duration) (bool, error)
}

func (c *schedulerClaimHandleStub) Renew(ctx context.Context, ttl time.Duration) (bool, error) {
	c.renewCalls++
	if c.renewFn != nil {
		return c.renewFn(ctx, ttl)
	}
	return true, nil
}
func (c *schedulerClaimHandleStub) MarkCompleted(ctx context.Context, _ time.Duration) (bool, error) {
	c.markCompletedCalls++
	c.markCompletedCtxErr = ctx.Err()
	return true, nil
}
func (c *schedulerClaimHandleStub) Release(ctx context.Context) (bool, error) {
	c.releaseCalls++
	c.releaseCtxErr = ctx.Err()
	return true, nil
}

type schedulerBudgetLimiterStub struct {
	mu          sync.Mutex
	decision    polling.BudgetDecision
	reservation *schedulerBudgetReservationStub
	err         error
	calls       int
	job         polling.BudgetJob
	profile     polling.BudgetProfile
	ttl         time.Duration
	ctxErr      error
}

func (l *schedulerBudgetLimiterStub) TryReserve(
	ctx context.Context,
	job *polling.BudgetJob,
	profile polling.BudgetProfile,
	ttl time.Duration,
) (polling.BudgetReservation, polling.BudgetDecision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if job != nil {
		l.job = *job
	}
	l.profile = profile
	l.ttl = ttl
	l.ctxErr = ctx.Err()
	if l.reservation != nil {
		return l.reservation, l.decision, l.err
	}
	return nil, l.decision, l.err
}

func (l *schedulerBudgetLimiterStub) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

type schedulerBudgetReservationStub struct {
	mu            sync.Mutex
	commitCalls   int
	releaseCalls  int
	commitCtxErr  error
	releaseCtxErr error
	commitErr     error
	releaseErr    error
}

func (r *schedulerBudgetReservationStub) Commit(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commitCalls++
	r.commitCtxErr = ctx.Err()
	return r.commitErr
}

func (r *schedulerBudgetReservationStub) Release(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releaseCalls++
	r.releaseCtxErr = ctx.Err()
	return r.releaseErr
}

func testBudgetProfile() polling.BudgetProfile {
	return polling.BudgetProfile{
		SourceUnits: map[polling.BudgetSource]float64{
			polling.BudgetSourceYouTubeScraper: 1,
		},
		BurstClass: polling.BudgetBurstPrimary,
		Priority:   polling.BudgetPriorityHigh,
	}
}

type sharedSchedulerClaimState struct {
	mu            sync.Mutex
	owner         bool
	completed     bool
	results       map[polling.JobClaimResult]int
	markCompleted int
	releases      int
}

func newSharedSchedulerClaimState() *sharedSchedulerClaimState {
	return &sharedSchedulerClaimState{results: make(map[polling.JobClaimResult]int)}
}

func (s *sharedSchedulerClaimState) TryClaim(
	context.Context,
	string,
	string,
	time.Duration,
	time.Duration,
) (polling.JobClaimStatus, polling.JobClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completed {
		s.results[polling.JobClaimAlreadyCompleted]++
		return polling.JobClaimStatus{Result: polling.JobClaimAlreadyCompleted, RetryAfter: time.Minute}, nil, nil
	}
	if s.owner {
		s.results[polling.JobClaimPeerOwned]++
		return polling.JobClaimStatus{Result: polling.JobClaimPeerOwned, RetryAfter: time.Minute}, nil, nil
	}
	s.owner = true
	s.results[polling.JobClaimAcquired]++
	return polling.JobClaimStatus{Result: polling.JobClaimAcquired}, &sharedSchedulerClaimHandle{state: s}, nil
}

func (s *sharedSchedulerClaimState) resultCount(result polling.JobClaimResult) int {
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
