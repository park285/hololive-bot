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
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
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
	s.mu.Lock()
	defer s.mu.Unlock()

	key := channelID + ":" + poller.Name()
	if _, exists := s.jobMap[key]; exists {
		return // 중복 등록 방지
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

	pollerName := targetSync.Poller.Name()
	desired := make(map[string]struct{}, len(targetSync.ChannelIDs))
	for _, channelID := range targetSync.ChannelIDs {
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
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	slog.Info("Scheduler starting",
		"worker_count", s.workerCount,
		"job_count", len(s.jobs),
		"poll_timeout", s.pollTimeout,
		"error_backoff_min", s.errorBackoffMin,
		"error_backoff_max", s.errorBackoffMax)

	// 작업 채널
	jobCh := make(chan *Job, s.workerCount*2)

	// 워커 시작
	for i := range s.workerCount {
		s.wg.Add(1)
		go s.worker(ctx, jobCh, i)
	}

	// 디스패처 시작
	s.wg.Add(1)
	go s.dispatcher(ctx, jobCh)
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopCh)
	s.wg.Wait()
	slog.Info("Scheduler stopped")
}

// dispatcher: 실행 대기 작업을 워커에게 전달
func (s *Scheduler) dispatcher(ctx context.Context, jobCh chan<- *Job) {
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
		case <-s.stopCh:
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
func (s *Scheduler) worker(ctx context.Context, jobCh <-chan *Job, id int) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
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
		job.NextRunAt = nextErrorRetryAt(now, job.Interval, job.consecutiveFailures, s.errorBackoffMin, s.errorBackoffMax)
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

func nextErrorRetryAt(now time.Time, interval time.Duration, consecutiveFailures int, minBackoff, maxBackoff time.Duration) time.Time {
	delay := errorRetryDelay(interval, consecutiveFailures, minBackoff, maxBackoff)
	return now.Add(delay)
}

func errorRetryDelay(interval time.Duration, consecutiveFailures int, minBackoff, maxBackoff time.Duration) time.Duration {
	if minBackoff <= 0 {
		minBackoff = 30 * time.Second
	}
	if maxBackoff <= 0 {
		maxBackoff = 5 * time.Minute
	}
	if maxBackoff < minBackoff {
		maxBackoff = minBackoff
	}

	if consecutiveFailures <= 1 {
		if interval > 0 && interval < minBackoff {
			return interval
		}
		return minBackoff
	}

	delay := minBackoff
	for i := 1; i < consecutiveFailures; i++ {
		if delay >= maxBackoff/2 {
			delay = maxBackoff
			break
		}
		delay *= 2
	}

	if interval > 0 && interval < delay {
		delay = interval
	}
	if delay > maxBackoff {
		delay = maxBackoff
	}
	return delay
}

func (s *Scheduler) notifyDispatcher() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func advanceNextRunAt(scheduledAt time.Time, interval time.Duration, now time.Time) time.Time {
	if interval <= 0 {
		return now
	}
	if scheduledAt.IsZero() {
		return now
	}
	if scheduledAt.After(now) {
		return scheduledAt
	}

	skipped := now.Sub(scheduledAt)/interval + 1
	return scheduledAt.Add(time.Duration(int64(skipped) * interval.Nanoseconds()))
}

func nextPollAt(now time.Time, interval, offset time.Duration) time.Time {
	if interval <= 0 {
		return now
	}

	if offset < 0 {
		offset = 0
	}
	if offset >= interval {
		offset %= interval
	}

	next := now.Truncate(interval).Add(offset)
	if next.After(now) {
		return next
	}

	return next.Add(interval)
}

// calculateOffset: 채널별 분산 오프셋 계산
func calculateOffset(key string, interval time.Duration) time.Duration {
	h := sha256.Sum256([]byte(key))
	fraction := float64(binary.BigEndian.Uint32(h[:4])) / float64(^uint32(0))
	return time.Duration(float64(interval) * fraction)
}

// jobHeap: 우선순위 큐 (min-heap by NextRunAt)
type jobHeap []*Job

func (h jobHeap) Len() int { return len(h) }

func (h jobHeap) Less(i, j int) bool {
	// 먼저 NextRunAt 비교
	if !h[i].NextRunAt.Equal(h[j].NextRunAt) {
		return h[i].NextRunAt.Before(h[j].NextRunAt)
	}
	// 같으면 우선순위 높은 것 먼저
	return h[i].Priority > h[j].Priority
}

func (h jobHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *jobHeap) Push(x any) {
	n := len(*h)
	job := x.(*Job)
	job.index = n
	*h = append(*h, job)
}

func (h *jobHeap) Pop() any {
	old := *h
	n := len(old)
	job := old[n-1]
	old[n-1] = nil
	job.index = -1
	*h = old[0 : n-1]
	return job
}

type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	lastTime time.Time
}

func NewRateLimiter(interval time.Duration) *RateLimiter {
	return &RateLimiter{
		interval: interval,
	}
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if r.lastTime.IsZero() {
		r.lastTime = now
		return nil
	}

	elapsed := now.Sub(r.lastTime)
	if elapsed < r.interval {
		waitTime := r.interval - elapsed
		timer := time.NewTimer(waitTime)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return fmt.Errorf("rate limit wait canceled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	r.lastTime = time.Now()
	return nil
}
