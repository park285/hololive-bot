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

// Package poller: YouTube 채널 데이터 폴링 및 스케줄링
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
)

type Poller interface {
	Poll(ctx context.Context, channelID string) error
	Name() string
}

type proxyTogglePoller interface {
	Poller
	SetProxyEnabled(enabled bool) bool
	ProxyEnabled() bool
}

type Job struct {
	ChannelID           string
	Poller              Poller
	Priority            Priority
	NextRunAt           time.Time
	Interval            time.Duration
	Offset              time.Duration
	key                 string
	budgetProfile       polling.BudgetProfile
	retired             bool
	immediateFirstRun   bool
	consecutiveFailures int
	index               int // heap 인덱스
}

type Priority int

const (
	PriorityLow    Priority = 0
	PriorityNormal Priority = 1
	PriorityHigh   Priority = 2
	PriorityBoost  Priority = 3 // 마일스톤 임박, LIVE 등
)

type Scheduler struct {
	mu                     sync.Mutex
	jobs                   jobHeap
	jobMap                 map[string]*Job // key: channelID:pollerName
	rateLimiter            *RateLimiter
	workerCount            int
	pollTimeout            time.Duration
	errorBackoffMin        time.Duration
	errorBackoffMax        time.Duration
	jobClaimer             polling.JobClaimer
	budgetLimiter          polling.GlobalBudgetLimiter
	budgetContext          polling.BudgetContext
	budgetAcquireTimeout   time.Duration
	claimLeaseSafetyMargin time.Duration
	claimCompletionTimeout time.Duration
	metrics                *polling.Metrics
	logger                 *slog.Logger
	stopCh                 chan struct{}
	stopCancel             context.CancelFunc
	wakeCh                 chan struct{}
	wg                     sync.WaitGroup
	running                bool
}

type PollerTargetSync struct {
	Poller                 Poller
	Priority               Priority
	Interval               time.Duration
	ChannelIDs             []string
	BudgetProfile          polling.BudgetProfile
	ForceImmediateFirstRun bool
}

type SchedulerConfig struct {
	WorkerCount            int           // 동시 워커 수 (기본: 4)
	RequestInterval        time.Duration // 요청 간 최소 간격 (기본: 4초)
	PollTimeout            time.Duration // 폴러 1회 실행 최대 시간 (기본: 45초)
	ErrorBackoffMin        time.Duration // 실패 후 최소 재시도 지연 (기본: 30초)
	ErrorBackoffMax        time.Duration // 실패 후 최대 재시도 지연 (기본: 5분)
	JobClaimer             polling.JobClaimer
	BudgetLimiter          polling.GlobalBudgetLimiter
	BudgetContext          polling.BudgetContext
	BudgetAcquireTimeout   time.Duration
	ClaimLeaseSafetyMargin time.Duration
	ClaimCompletionTimeout time.Duration
	Metrics                *polling.Metrics
	Logger                 *slog.Logger // nil이면 slog.Default()로 폴백
}

func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		WorkerCount:            4,
		RequestInterval:        4 * time.Second,
		PollTimeout:            45 * time.Second,
		ErrorBackoffMin:        30 * time.Second,
		ErrorBackoffMax:        5 * time.Minute,
		BudgetAcquireTimeout:   3 * time.Second,
		ClaimLeaseSafetyMargin: 15 * time.Second,
		ClaimCompletionTimeout: 5 * time.Second,
	}
}

// WorkerCount는 현재 스케줄러의 워커 수를 반환한다.
func (s *Scheduler) WorkerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.workerCount
}

func applySchedulerWorkerDefaults(config, defaults SchedulerConfig) SchedulerConfig {
	if config.WorkerCount <= 0 {
		config.WorkerCount = defaults.WorkerCount
	}
	if config.PollTimeout <= 0 {
		config.PollTimeout = defaults.PollTimeout
	}
	if config.ErrorBackoffMin <= 0 {
		config.ErrorBackoffMin = defaults.ErrorBackoffMin
	}
	if config.ErrorBackoffMax <= 0 {
		config.ErrorBackoffMax = defaults.ErrorBackoffMax
	}
	if config.ErrorBackoffMax < config.ErrorBackoffMin {
		config.ErrorBackoffMax = config.ErrorBackoffMin
	}
	return config
}

func applySchedulerClaimBudgetDefaults(config, defaults SchedulerConfig) SchedulerConfig {
	if config.BudgetAcquireTimeout <= 0 {
		config.BudgetAcquireTimeout = defaults.BudgetAcquireTimeout
	}
	if config.ClaimLeaseSafetyMargin <= 0 {
		config.ClaimLeaseSafetyMargin = defaults.ClaimLeaseSafetyMargin
	}
	if config.ClaimCompletionTimeout <= 0 {
		config.ClaimCompletionTimeout = defaults.ClaimCompletionTimeout
	}
	return config
}

func NewScheduler(config SchedulerConfig) *Scheduler {
	defaults := DefaultSchedulerConfig()
	config = applySchedulerWorkerDefaults(config, defaults)
	config = applySchedulerClaimBudgetDefaults(config, defaults)
	// RequestInterval이 0이면 NewRateLimiter(0)이 생성되어 Wait()가 즉시 반환.
	// 외부 RateLimiter에 rate limiting을 위임하는 경우에 사용.
	metrics := config.Metrics
	if metrics == nil {
		metrics = polling.NewMetrics()
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Scheduler{
		jobs:                   make(jobHeap, 0),
		jobMap:                 make(map[string]*Job),
		rateLimiter:            NewRateLimiter(config.RequestInterval),
		workerCount:            config.WorkerCount,
		pollTimeout:            config.PollTimeout,
		errorBackoffMin:        config.ErrorBackoffMin,
		errorBackoffMax:        config.ErrorBackoffMax,
		jobClaimer:             config.JobClaimer,
		budgetLimiter:          config.BudgetLimiter,
		budgetContext:          config.BudgetContext,
		budgetAcquireTimeout:   config.BudgetAcquireTimeout,
		claimLeaseSafetyMargin: config.ClaimLeaseSafetyMargin,
		claimCompletionTimeout: config.ClaimCompletionTimeout,
		metrics:                metrics,
		logger:                 logger,
		stopCh:                 make(chan struct{}),
		wakeCh:                 make(chan struct{}, 1),
	}
}

func (s *Scheduler) Metrics() *polling.Metrics {
	return s.metrics
}
