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

package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type scrapeTriggerType string

const (
	scrapeTriggerRegular scrapeTriggerType = "regular"
	scrapeTriggerRetry   scrapeTriggerType = "retry"
)

// FeedScheduler는 RSS 수집 주기 실행과 재시도를 담당한다.
type FeedScheduler struct {
	service *Service
	config  FeedScheduleConfig
	logger  *slog.Logger
	nowFn   func() time.Time

	retryMu   sync.Mutex
	retryRuns []time.Time

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewFeedScheduler는 FeedScheduler를 생성한다.
func NewFeedScheduler(service *Service, cfg FeedScheduleConfig, logger *slog.Logger) (*FeedScheduler, error) {
	if service == nil {
		return nil, fmt.Errorf("new feed scheduler: service is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	normalized := cfg
	defaults := DefaultFeedScheduleConfig()
	if normalized.RunTimeout <= 0 {
		normalized.RunTimeout = defaults.RunTimeout
	}
	if len(normalized.RetryDelays) == 0 {
		normalized.RetryDelays = defaults.RetryDelays
	}

	return &FeedScheduler{
		service: service,
		config:  normalized,
		logger:  logger,
		nowFn:   time.Now,
		stopCh:  make(chan struct{}),
	}, nil
}

// Start는 스케줄러를 시작한다.
func (s *FeedScheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop은 스케줄러를 종료한다.
func (s *FeedScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *FeedScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	for {
		now := s.now()
		nextRun, trigger := s.nextRun(now)
		waitDuration := time.Until(nextRun)
		if waitDuration < 0 {
			waitDuration = 0
		}

		s.logger.Info(
			"Major event feed scheduler waiting",
			slog.String("next_run_kst", formatKST(nextRun)),
			slog.String("trigger", string(trigger)),
			slog.Duration("wait_duration", waitDuration),
		)

		timer := time.NewTimer(waitDuration)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.logger.Info("Major event feed scheduler stopped by context")
			return
		case <-s.stopCh:
			timer.Stop()
			s.logger.Info("Major event feed scheduler stopped")
			return
		case <-timer.C:
		}

		s.executeCycle(ctx, trigger, nextRun)
	}
}

func (s *FeedScheduler) executeCycle(ctx context.Context, trigger scrapeTriggerType, scheduledAt time.Time) {
	runCtx, cancel := context.WithTimeout(ctx, s.config.RunTimeout)
	defer cancel()

	result, err := s.service.Scrape(runCtx)
	completedAt := s.now()

	if err != nil {
		s.handleFailedScrape(trigger, scheduledAt, completedAt, err)
		return
	}

	cleared := s.clearRetryRuns()
	s.logger.Info(
		"Major event feed scrape completed",
		slog.String("trigger", string(trigger)),
		slog.Int("feeds_attempted", result.FeedsAttempted),
		slog.Int("feeds_failed", result.FeedsFailed),
		slog.Int("parsed_events", result.ParsedEvents),
		slog.Int("stored_events", result.StoredEvents),
		slog.Int("skipped_known", result.SkippedKnown),
		slog.Int("cleared_retries", cleared),
	)
}

func (s *FeedScheduler) handleFailedScrape(
	trigger scrapeTriggerType,
	scheduledAt time.Time,
	completedAt time.Time,
	scrapeErr error,
) {
	if trigger == scrapeTriggerRetry {
		s.logger.Warn(
			"Major event feed retry scrape failed",
			slog.String("scheduled_run_kst", formatKST(scheduledAt)),
			slog.String("failed_at_kst", formatKST(completedAt)),
			slog.String("error", scrapeErr.Error()),
			slog.Int("remaining_retries", s.retryRunCount()),
		)
		return
	}

	retryRuns := buildRetryRuns(scheduledAt, completedAt, s.config.RetryDelays)
	s.setRetryRuns(retryRuns)

	s.logger.Warn(
		"Major event feed scrape failed",
		slog.String("scheduled_run_kst", formatKST(scheduledAt)),
		slog.String("failed_at_kst", formatKST(completedAt)),
		slog.String("error", scrapeErr.Error()),
		slog.Int("retry_count", len(retryRuns)),
	)
}

func (s *FeedScheduler) nextRun(now time.Time) (time.Time, scrapeTriggerType) {
	nextRegular := calculateNextRunAtHour(now, s.config.ScrapeHourKST)
	nextRetry := s.peekRetryRun()

	if !nextRetry.IsZero() && (nextRetry.Before(nextRegular) || nextRetry.Equal(nextRegular)) {
		s.popRetryRun()
		return nextRetry, scrapeTriggerRetry
	}
	return nextRegular, scrapeTriggerRegular
}

func (s *FeedScheduler) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn().UTC()
	}
	return time.Now().UTC()
}

func (s *FeedScheduler) setRetryRuns(runs []time.Time) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	if len(runs) == 0 {
		s.retryRuns = nil
		return
	}

	s.retryRuns = append([]time.Time(nil), runs...)
}

func (s *FeedScheduler) clearRetryRuns() int {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	cleared := len(s.retryRuns)
	s.retryRuns = nil
	return cleared
}

func (s *FeedScheduler) peekRetryRun() time.Time {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	if len(s.retryRuns) == 0 {
		return time.Time{}
	}
	return s.retryRuns[0]
}

func (s *FeedScheduler) popRetryRun() {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	if len(s.retryRuns) == 0 {
		return
	}
	s.retryRuns = s.retryRuns[1:]
}

func (s *FeedScheduler) retryRunCount() int {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()
	return len(s.retryRuns)
}
