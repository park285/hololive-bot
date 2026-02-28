package app

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
)

func buildMajorEventComponents(
	ctx context.Context,
	majorEventCfg config.MajorEventConfig,
	majorEventRepo *majorevent.Repository,
	formatter *adapter.ResponseFormatter,
	summarizer *majorevent.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepo *delivery.OutboxRepository,
	logger *slog.Logger,
	autoPrepareSchema bool,
) (*majorevent.Scheduler, *majorevent.MonthlyScheduler, *majorevent.ScraperScheduler) {
	majorEventScheduler := majorevent.NewScheduler(
		majorEventRepo,
		formatter,
		summarizer,
		locker,
		outboxRepo,
		logger,
	)

	majorEventMonthlyScheduler := majorevent.NewMonthlyScheduler(
		majorEventRepo,
		formatter,
		summarizer,
		locker,
		outboxRepo,
		logger,
	)

	if autoPrepareSchema {
		if err := majorEventRepo.CreateEventsTable(ctx); err != nil {
			logger.Error("Failed to create major_events table", slog.String("error", err.Error()))
		}
	}

	if !majorEventCfg.ScraperEnabled {
		logger.Info("Major event scraper scheduler disabled by config",
			slog.Int("scrape_hour_kst", majorEventCfg.ScrapeHourKST))
		return majorEventScheduler, majorEventMonthlyScheduler, nil
	}

	majorEventScraper := majorevent.NewScraper(
		providers.ProvideMajorEventHTTPClient(),
		majorEventRepo,
		majorevent.WithScraperLogger(logger),
	)
	linkChecker := majorevent.NewLinkChecker(
		providers.ProvideMajorEventHTTPClient(),
		majorEventRepo,
		logger,
	)
	majorEventScraperScheduler := majorevent.NewScraperScheduler(
		majorEventScraper,
		majorEventRepo,
		linkChecker,
		logger,
		majorevent.WithScraperSchedulerHour(majorEventCfg.ScrapeHourKST),
	)

	return majorEventScheduler, majorEventMonthlyScheduler, majorEventScraperScheduler
}

func buildMemberNewsComponents(memberNews *membernews.Service, formatter *adapter.ResponseFormatter, locker delivery.NotificationLocker, outboxRepo *delivery.OutboxRepository, logger *slog.Logger) (*membernews.Scheduler, *membernews.MonthlyScheduler) {
	if memberNews == nil {
		logger.Info("Member news scheduler disabled: service unavailable")
		return nil, nil
	}

	scheduler := membernews.NewScheduler(
		memberNews,
		formatter,
		locker,
		outboxRepo,
		logger,
	)
	monthlyScheduler := membernews.NewMonthlyScheduler(
		memberNews,
		formatter,
		locker,
		outboxRepo,
		logger,
	)
	return scheduler, monthlyScheduler
}
