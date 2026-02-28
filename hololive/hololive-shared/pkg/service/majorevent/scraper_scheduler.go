package majorevent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type ScraperScheduler struct {
	scraper     *Scraper
	repository  *Repository
	linkChecker *LinkChecker
	logger      *slog.Logger
	scrapeHour  *int
	scrapeMu    sync.RWMutex

	retryMu   sync.Mutex
	retryRuns []time.Time

	stopCh   chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

type scrapeTriggerType string

const (
	scrapeTriggerRegular scrapeTriggerType = "regular"
	scrapeTriggerRetry   scrapeTriggerType = "retry"
	scrapeTriggerManual  scrapeTriggerType = "manual"
)

type ScraperSchedulerOption func(*ScraperScheduler)

func WithScraperSchedulerHour(scrapeHourKST int) ScraperSchedulerOption {
	return func(s *ScraperScheduler) {
		hour := normalizeScrapeHourKST(scrapeHourKST)
		s.scrapeHour = &hour
	}
}

func NewScraperScheduler(
	scraper *Scraper,
	repository *Repository,
	linkChecker *LinkChecker,
	logger *slog.Logger,
	opts ...ScraperSchedulerOption,
) *ScraperScheduler {
	scheduler := &ScraperScheduler{
		scraper:     scraper,
		repository:  repository,
		linkChecker: linkChecker,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(scheduler)
	}

	return scheduler
}

func (s *ScraperScheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

func (s *ScraperScheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *ScraperScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	for {
		nextRun, triggerType := s.nextRunWithTrigger(time.Now())
		waitDuration := time.Until(nextRun)
		if waitDuration < 0 {
			waitDuration = 0
		}

		s.logger.Info("Scraper scheduler waiting",
			slog.Time("next_run", nextRun),
			slog.String("trigger_type", string(triggerType)),
			slog.Duration("wait_duration", waitDuration))

		timer := time.NewTimer(waitDuration)

		select {
		case <-ctx.Done():
			timer.Stop()
			s.logger.Info("Scraper scheduler stopped by context")
			return
		case <-s.stopCh:
			timer.Stop()
			s.logger.Info("Scraper scheduler stopped")
			return
		case <-timer.C:
			scheduledAt := nextRun
			if triggerType == scrapeTriggerRetry {
				retryRun, ok := s.popNextRetryRun()
				if !ok {
					s.logger.Warn("Retry trigger fired but retry queue was empty",
						slog.Time("scheduled_run", nextRun))
					continue
				}
				scheduledAt = retryRun
			}

			scrapeErr := s.runScrape(ctx)
			s.handleScrapeResult(triggerType, scheduledAt, time.Now(), scrapeErr)
		}
	}
}

func (s *ScraperScheduler) runScrape(ctx context.Context) error {
	s.logger.Info("Starting major event scrape")

	expired, err := s.repository.UpdateExpiredEvents(ctx)
	if err != nil {
		s.logger.Error("Failed to update expired events", slog.String("error", err.Error()))
	} else if expired > 0 {
		s.logger.Info("Updated expired events", slog.Int64("count", expired))
	}

	stored, scrapeErr := s.scraper.ScrapeAndStore(ctx)
	if scrapeErr != nil {
		s.logger.Error("Failed to scrape events", slog.String("error", scrapeErr.Error()))
	} else {
		s.logger.Info("Scrape completed", slog.Int("stored", stored))
	}

	if s.linkChecker == nil {
		return scrapeErr
	}

	result, err := s.linkChecker.CheckStaleLinks(ctx)
	if err != nil {
		s.logger.Error("Failed to check major event links",
			slog.Int("checked", result.Checked),
			slog.Int("ok", result.OK),
			slog.Int("failed", result.Failed),
			slog.Int("blocked", result.Blocked),
			slog.String("error", err.Error()))
		return scrapeErr
	}

	if result.Checked > 0 {
		s.logger.Info("Major event link check completed",
			slog.Int("checked", result.Checked),
			slog.Int("ok", result.OK),
			slog.Int("failed", result.Failed),
			slog.Int("blocked", result.Blocked))
	}

	return scrapeErr
}

func (s *ScraperScheduler) calculateNextRun(now time.Time) time.Time {
	nextRun, _ := s.nextRunWithTrigger(now)
	return nextRun
}

func (s *ScraperScheduler) nextRunWithTrigger(now time.Time) (time.Time, scrapeTriggerType) {
	nextRegular := s.calculateNextRegularRun(now)

	nextRetry, ok := s.peekNextRetryRun()
	if ok && !nextRetry.After(nextRegular) {
		return nextRetry, scrapeTriggerRetry
	}

	return nextRegular, scrapeTriggerRegular
}

func (s *ScraperScheduler) calculateNextRegularRun(now time.Time) time.Time {
	nowKST := now.In(kst)

	scrapeHour := s.ScrapeHourKST()

	targetDate := time.Date(
		nowKST.Year(), nowKST.Month(), nowKST.Day(),
		scrapeHour, 0, 0, 0, kst,
	)

	if !targetDate.After(nowKST) {
		targetDate = targetDate.AddDate(0, 0, 1)
	}

	return targetDate
}

func (s *ScraperScheduler) ScrapeHourKST() int {
	if s == nil {
		return constants.MajorEventConfig.ScrapeHourKST
	}

	s.scrapeMu.RLock()
	defer s.scrapeMu.RUnlock()

	if s.scrapeHour == nil {
		return constants.MajorEventConfig.ScrapeHourKST
	}

	return normalizeScrapeHourKST(*s.scrapeHour)
}

// SetScrapeHourKST: 스크래퍼 정기 실행 시각(KST)을 런타임에 갱신합니다.
// 반환값은 정규화(clamp) 후 실제 적용된 시각입니다.
func (s *ScraperScheduler) SetScrapeHourKST(hour int) int {
	applied := normalizeScrapeHourKST(hour)
	if s == nil {
		return applied
	}

	s.scrapeMu.Lock()
	defer s.scrapeMu.Unlock()
	s.scrapeHour = &applied

	return applied
}

func (s *ScraperScheduler) handleScrapeResult(triggerType scrapeTriggerType, scheduledAt, completedAt time.Time, scrapeErr error) {
	if scrapeErr == nil {
		cleared := s.clearRetryRuns()
		if cleared > 0 {
			s.logger.Info("Scrape succeeded; cleared retry queue",
				slog.String("trigger_type", string(triggerType)),
				slog.Int("cleared_retries", cleared))
		}
		return
	}

	if triggerType != scrapeTriggerRegular {
		s.logger.Warn("Retry scrape failed",
			slog.Time("scheduled_run", scheduledAt),
			slog.Time("failed_at", completedAt),
			slog.Int("remaining_retries", s.retryRunCount()),
			slog.String("error", scrapeErr.Error()))
		return
	}

	retryRuns := s.buildRetryRuns(scheduledAt, completedAt)
	s.setRetryRuns(retryRuns)

	if len(retryRuns) == 0 {
		s.logger.Warn("Scheduled scrape failed; no same-day retries queued",
			slog.Time("scheduled_run", scheduledAt),
			slog.Time("failed_at", completedAt),
			slog.String("error", scrapeErr.Error()))
		return
	}

	s.logger.Warn("Scheduled scrape failed; retry queue updated",
		slog.Time("scheduled_run", scheduledAt),
		slog.Time("failed_at", completedAt),
		slog.Int("retry_count", len(retryRuns)),
		slog.Any("retry_runs", retryRuns),
		slog.String("error", scrapeErr.Error()))
}

func (s *ScraperScheduler) buildRetryRuns(baseRun, failedAt time.Time) []time.Time {
	baseKST := baseRun.In(kst)
	failedAtKST := failedAt.In(kst)

	retryRuns := make([]time.Time, 0, len(constants.MajorEventConfig.ScrapeRetryDelays))
	for _, delay := range constants.MajorEventConfig.ScrapeRetryDelays {
		if delay <= 0 {
			continue
		}

		candidate := baseKST.Add(delay).In(kst)
		if candidate.Year() != baseKST.Year() || candidate.YearDay() != baseKST.YearDay() {
			continue
		}
		if !candidate.After(failedAtKST) {
			continue
		}

		retryRuns = append(retryRuns, candidate)
	}

	return retryRuns
}

func (s *ScraperScheduler) setRetryRuns(runs []time.Time) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	if len(runs) == 0 {
		s.retryRuns = nil
		return
	}

	s.retryRuns = append([]time.Time(nil), runs...)
}

func (s *ScraperScheduler) clearRetryRuns() int {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	cleared := len(s.retryRuns)
	s.retryRuns = nil
	return cleared
}

func (s *ScraperScheduler) peekNextRetryRun() (time.Time, bool) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	if len(s.retryRuns) == 0 {
		return time.Time{}, false
	}

	return s.retryRuns[0], true
}

func (s *ScraperScheduler) popNextRetryRun() (time.Time, bool) {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	if len(s.retryRuns) == 0 {
		return time.Time{}, false
	}

	next := s.retryRuns[0]
	s.retryRuns = s.retryRuns[1:]
	if len(s.retryRuns) == 0 {
		s.retryRuns = nil
	}

	return next, true
}

func (s *ScraperScheduler) retryRunCount() int {
	s.retryMu.Lock()
	defer s.retryMu.Unlock()

	return len(s.retryRuns)
}

func (s *ScraperScheduler) RunNow(ctx context.Context) {
	scrapeErr := s.runScrape(ctx)
	now := time.Now()
	s.handleScrapeResult(scrapeTriggerManual, now, now, scrapeErr)
}

func normalizeScrapeHourKST(hour int) int {
	if hour < 0 {
		return 0
	}
	if hour > 23 {
		return 23
	}
	return hour
}
