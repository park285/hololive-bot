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
package poller

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
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
	mu              sync.Mutex
	jobs            jobHeap
	jobMap          map[string]*Job // key: channelID:pollerName
	rateLimiter     *RateLimiter
	workerCount     int
	pollTimeout     time.Duration
	errorBackoffMin time.Duration
	errorBackoffMax time.Duration
	stopCh          chan struct{}
	stopCancel      context.CancelFunc
	wakeCh          chan struct{}
	wg              sync.WaitGroup
	running         bool
}

type PollerTargetSync struct {
	Poller                 Poller
	Priority               Priority
	Interval               time.Duration
	ChannelIDs             []string
	ForceImmediateFirstRun bool
}

type SchedulerConfig struct {
	WorkerCount     int           // 동시 워커 수 (기본: 4)
	RequestInterval time.Duration // 요청 간 최소 간격 (기본: 4초)
	PollTimeout     time.Duration // 폴러 1회 실행 최대 시간 (기본: 45초)
	ErrorBackoffMin time.Duration // 실패 후 최소 재시도 지연 (기본: 30초)
	ErrorBackoffMax time.Duration // 실패 후 최대 재시도 지연 (기본: 5분)
}

func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		WorkerCount:     4,
		RequestInterval: 4 * time.Second,
		PollTimeout:     45 * time.Second,
		ErrorBackoffMin: 30 * time.Second,
		ErrorBackoffMax: 5 * time.Minute,
	}
}

// WorkerCount는 현재 스케줄러의 워커 수를 반환한다.
func (s *Scheduler) WorkerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.workerCount
}

func NewScheduler(cfg SchedulerConfig) *Scheduler {
	defaults := DefaultSchedulerConfig()
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = defaults.WorkerCount
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = defaults.PollTimeout
	}
	if cfg.ErrorBackoffMin <= 0 {
		cfg.ErrorBackoffMin = defaults.ErrorBackoffMin
	}
	if cfg.ErrorBackoffMax <= 0 {
		cfg.ErrorBackoffMax = defaults.ErrorBackoffMax
	}
	if cfg.ErrorBackoffMax < cfg.ErrorBackoffMin {
		cfg.ErrorBackoffMax = cfg.ErrorBackoffMin
	}
	// RequestInterval이 0이면 NewRateLimiter(0)이 생성되어 Wait()가 즉시 반환.
	// 외부 RateLimiter에 rate limiting을 위임하는 경우에 사용.
	ensureMetrics()

	return &Scheduler{
		jobs:            make(jobHeap, 0),
		jobMap:          make(map[string]*Job),
		rateLimiter:     NewRateLimiter(cfg.RequestInterval),
		workerCount:     cfg.WorkerCount,
		pollTimeout:     cfg.PollTimeout,
		errorBackoffMin: cfg.ErrorBackoffMin,
		errorBackoffMax: cfg.ErrorBackoffMax,
		stopCh:          make(chan struct{}),
		wakeCh:          make(chan struct{}, 1),
	}
}

func (s *Scheduler) Register(channelID string, poller Poller, priority Priority, interval time.Duration) {
	if err := s.RegisterChecked(channelID, poller, priority, interval); err != nil {
		slog.Warn("Skip invalid scheduler registration",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)
	}
}

func (s *Scheduler) RegisterChecked(channelID string, poller Poller, priority Priority, interval time.Duration) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return fmt.Errorf("channel id is empty")
	}
	if poller == nil {
		return fmt.Errorf("poller is nil")
	}
	if interval <= 0 {
		return fmt.Errorf("interval must be positive: %s", interval)
	}

	pollerName := strings.TrimSpace(poller.Name())
	if pollerName == "" {
		return fmt.Errorf("poller name is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := channelID + ":" + pollerName
	if _, exists := s.jobMap[key]; exists {
		return nil // 중복 등록 방지
	}

	offset := calculateOffset(key, interval)
	job := &Job{
		ChannelID: channelID,
		Poller:    poller,
		Priority:  priority,
		NextRunAt: nextPollAt(time.Now(), interval, offset),
		Interval:  interval,
		Offset:    offset,
		key:       key,
	}

	heap.Push(&s.jobs, job)
	s.jobMap[key] = job
	schedulerRegisteredJobs.Set(float64(len(s.jobMap)))
	s.notifyDispatcher()
	return nil
}

func (s *Scheduler) UpdatePriority(channelID string, pollerName string, priority Priority, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := channelID + ":" + pollerName
	job, exists := s.jobMap[key]
	if !exists {
		return
	}

	job.Priority = priority
	if job.Interval != interval && interval > 0 {
		s.resetJobScheduleForIntervalChange(job, interval)
	}
	job.Interval = interval
	if job.index >= 0 {
		heap.Fix(&s.jobs, job.index)
	}
	s.notifyDispatcher()
}

func (s *Scheduler) SyncPollerTargets(targetSync PollerTargetSync) {
	if targetSync.Poller == nil || targetSync.Interval <= 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pollerName := strings.TrimSpace(targetSync.Poller.Name())
	if pollerName == "" {
		return
	}

	desired := make(map[string]struct{}, len(targetSync.ChannelIDs))
	for _, channelID := range targetSync.ChannelIDs {
		channelID = strings.TrimSpace(channelID)
		if channelID == "" {
			continue
		}
		desired[channelID] = struct{}{}
	}

	for key, job := range s.jobMap {
		if job == nil || job.Poller == nil || job.Poller.Name() != pollerName {
			continue
		}

		if _, keep := desired[job.ChannelID]; !keep {
			job.retired = true
			if job.index >= 0 {
				heap.Remove(&s.jobs, job.index)
			}
			delete(s.jobMap, key)
			continue
		}

		job.Poller = targetSync.Poller
		job.Priority = targetSync.Priority
		if job.Interval != targetSync.Interval {
			s.resetJobScheduleForIntervalChange(job, targetSync.Interval)
		}
		job.Interval = targetSync.Interval
		if job.index >= 0 {
			heap.Fix(&s.jobs, job.index)
		}
		delete(desired, job.ChannelID)
	}

	now := time.Now()
	for channelID := range desired {
		key := channelID + ":" + pollerName
		offset := calculateOffset(key, targetSync.Interval)
		nextRunAt := nextPollAt(now, targetSync.Interval, offset)
		if targetSync.ForceImmediateFirstRun {
			nextRunAt = now
		}
		job := &Job{
			ChannelID:         channelID,
			Poller:            targetSync.Poller,
			Priority:          targetSync.Priority,
			NextRunAt:         nextRunAt,
			Interval:          targetSync.Interval,
			Offset:            offset,
			key:               key,
			immediateFirstRun: targetSync.ForceImmediateFirstRun,
		}
		heap.Push(&s.jobs, job)
		s.jobMap[key] = job
	}

	schedulerRegisteredJobs.Set(float64(len(s.jobMap)))
	s.notifyDispatcher()
}

func (s *Scheduler) resetJobScheduleForIntervalChange(job *Job, interval time.Duration) {
	if job == nil || interval <= 0 {
		return
	}

	job.consecutiveFailures = 0
	job.Offset = calculateOffset(job.key, interval)
	job.NextRunAt = nextPollAt(time.Now(), interval, job.Offset)
	job.immediateFirstRun = false
}

// 반환값은 토글 적용을 시도한 폴러 수입니다.
func (s *Scheduler) SetProxyEnabled(enabled bool) int {
	pollers := s.collectProxyTogglePollers()
	applied := 0
	for _, poller := range pollers {
		if poller.SetProxyEnabled(enabled) {
			applied++
		}
	}

	slog.Info("Scheduler proxy toggle applied",
		"enabled", enabled,
		"pollers", len(pollers),
		"applied", applied)

	return applied
}

// known=false이면 프록시 토글 지원 폴러가 없음을 의미합니다.
func (s *Scheduler) ProxyEnabled() (enabled bool, known bool) {
	pollers := s.collectProxyTogglePollers()
	if len(pollers) == 0 {
		return false, false
	}
	return pollers[0].ProxyEnabled(), true
}

func (s *Scheduler) collectProxyTogglePollers() []proxyTogglePoller {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[Poller]struct{})
	pollers := make([]proxyTogglePoller, 0)
	for _, job := range s.jobMap {
		if job == nil || job.Poller == nil {
			continue
		}
		if _, exists := seen[job.Poller]; exists {
			continue
		}
		seen[job.Poller] = struct{}{}

		toggler, ok := job.Poller.(proxyTogglePoller)
		if !ok {
			continue
		}
		pollers = append(pollers, toggler)
	}
	return pollers
}

func (s *Scheduler) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}

	stopCh := make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	s.stopCh = stopCh
	s.stopCancel = cancel
	s.running = true

	workerCount := s.workerCount
	jobCount := len(s.jobs)
	pollTimeout := s.pollTimeout
	errorBackoffMin := s.errorBackoffMin
	errorBackoffMax := s.errorBackoffMax
	s.mu.Unlock()

	slog.Info("Scheduler starting",
		"worker_count", workerCount,
		"job_count", jobCount,
		"poll_timeout", pollTimeout,
		"error_backoff_min", errorBackoffMin,
		"error_backoff_max", errorBackoffMax)

	// 작업 채널
	jobCh := make(chan *Job, workerCount*2)

	// 워커 시작
	for i := range workerCount {
		s.wg.Add(1)
		go s.worker(runCtx, jobCh, i, stopCh)
	}

	// 디스패처 시작
	s.wg.Add(1)
	go s.dispatcher(runCtx, jobCh, stopCh)
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	stopCh := s.stopCh
	stopCancel := s.stopCancel
	s.running = false
	s.stopCancel = nil
	s.mu.Unlock()

	if stopCancel != nil {
		stopCancel()
	}
	if stopCh != nil {
		close(stopCh)
	}
	s.wg.Wait()
	slog.Info("Scheduler stopped")
}

// dispatcher: 실행 대기 작업을 워커에게 전달
func (s *Scheduler) dispatcher(ctx context.Context, jobCh chan<- *Job, stopCh <-chan struct{}) {
	defer s.wg.Done()
	defer close(jobCh)

	timer := time.NewTimer(0)
	defer timer.Stop()
	workerChannelFull := false

	for {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(s.nextDispatchDelay(workerChannelFull))

		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-s.wakeCh:
			workerChannelFull = false
		case <-timer.C:
			workerChannelFull = s.dispatchDueJobs(jobCh)
		}
	}
}

func (s *Scheduler) nextDispatchDelay(workerChannelFull bool) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if workerChannelFull {
		return 50 * time.Millisecond
	}
	if len(s.jobs) == 0 {
		return time.Second
	}

	wait := time.Until(s.jobs[0].NextRunAt)
	if wait < 0 {
		return 0
	}
	return wait
}

// dispatchDueJobs: 실행 시간이 된 작업 전달
func (s *Scheduler) dispatchDueJobs(jobCh chan<- *Job) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for len(s.jobs) > 0 {
		job := s.jobs[0]
		if job.NextRunAt.After(now) {
			break
		}

		// 작업 추출
		heap.Pop(&s.jobs)

		// 워커에게 전달 (논블로킹)
		select {
		case jobCh <- job:
		default:
			// 채널 가득 참 - 현재 슬롯 anchor를 유지한 채 재시도한다.
			schedulerDispatchDefer.WithLabelValues("worker_channel_full").Inc()
			heap.Push(&s.jobs, job)
			return true
		}
	}

	return false
}

// worker: 작업 실행 워커
func (s *Scheduler) worker(ctx context.Context, jobCh <-chan *Job, id int, stopCh <-chan struct{}) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case job, ok := <-jobCh:
			if !ok {
				return
			}
			s.executeJob(ctx, job, id)
		}
	}
}

// executeJob: 작업 실행
func (s *Scheduler) executeJob(ctx context.Context, job *Job, workerID int) {
	// 레이트 리밋 대기
	if err := s.rateLimiter.Wait(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Debug("Rate limiter wait canceled", "error", err)
		} else {
			slog.Warn("Rate limiter wait failed", "error", err)
		}
		s.rescheduleJobAfterPoll(job, err)
		return
	}

	pollCtx := ctx
	cancel := func() {}
	if s.pollTimeout > 0 {
		pollCtx, cancel = context.WithTimeout(ctx, s.pollTimeout)
	}
	defer cancel()

	start := time.Now()
	err := job.Poller.Poll(pollCtx, job.ChannelID)
	elapsed := time.Since(start)
	status := "success"

	if err != nil {
		status = "error"
		if errors.Is(err, context.Canceled) {
			status = "canceled"
			slog.Debug("Poll canceled",
				"poller", job.Poller.Name(),
				"channel_id", job.ChannelID,
				"worker_id", workerID,
				"elapsed", elapsed)
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(pollCtx.Err(), context.DeadlineExceeded) {
			status = "timeout"
			slog.Warn("Poll timed out",
				"poller", job.Poller.Name(),
				"channel_id", job.ChannelID,
				"worker_id", workerID,
				"timeout", s.pollTimeout,
				"elapsed", elapsed,
				"error", err)
		} else {
			slog.Warn("Poll failed",
				"poller", job.Poller.Name(),
				"channel_id", job.ChannelID,
				"worker_id", workerID,
				"error", err,
				"elapsed", elapsed)
		}
	} else {
		slog.Debug("Poll succeeded",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"worker_id", workerID,
			"elapsed", elapsed)
	}
	schedulerPollDuration.WithLabelValues(job.Poller.Name(), status).Observe(elapsed.Seconds())

	s.rescheduleJobAfterPoll(job, err)
}

// rescheduleJob: 작업 재스케줄
func (s *Scheduler) rescheduleJob(job *Job) {
	s.rescheduleJobAfterPoll(job, nil)
}

type retryDelayError interface {
	RetryDelay() time.Duration
}

func (s *Scheduler) rescheduleJobAfterPoll(job *Job, pollErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job == nil || job.retired {
		return
	}
	current, ok := s.jobMap[job.key]
	if !ok || current != job {
		return
	}

	now := time.Now()
	if pollErr != nil && !errors.Is(pollErr, context.Canceled) {
		job.consecutiveFailures++

		var delayed retryDelayError
		if errors.As(pollErr, &delayed) && delayed.RetryDelay() > 0 {
			job.NextRunAt = now.Add(delayed.RetryDelay())
		} else {
			job.NextRunAt = nextErrorRetryAt(now, job.Interval, job.consecutiveFailures, s.errorBackoffMin, s.errorBackoffMax)
		}

		slog.Debug("Poll job rescheduled after failure",
			"poller", job.Poller.Name(),
			"channel_id", job.ChannelID,
			"consecutive_failures", job.consecutiveFailures,
			"next_run_at", job.NextRunAt)
	} else {
		hadFailures := job.consecutiveFailures > 0
		job.consecutiveFailures = 0

		if job.immediateFirstRun || hadFailures {
			job.NextRunAt = nextPollAt(now, job.Interval, job.Offset)
			job.immediateFirstRun = false
		} else {
			job.NextRunAt = advanceNextRunAt(job.NextRunAt, job.Interval, now)
		}
	}
	if job.index >= 0 {
		heap.Fix(&s.jobs, job.index)
	} else {
		heap.Push(&s.jobs, job)
	}
	s.notifyDispatcher()
}

func (s *Scheduler) notifyDispatcher() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}
