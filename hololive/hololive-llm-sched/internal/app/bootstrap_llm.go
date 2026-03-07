package app

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-llm-sched/internal/service/majorevent"
	mescheduler "github.com/kapu/hololive-llm-sched/internal/service/majorevent/scheduler"
	mescraper "github.com/kapu/hololive-llm-sched/internal/service/majorevent/scraper"
	mesummarizer "github.com/kapu/hololive-llm-sched/internal/service/majorevent/summarizer"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews"
	mnscheduler "github.com/kapu/hololive-llm-sched/internal/service/membernews/scheduler"

	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

func buildMajorEventComponents(
	ctx context.Context,
	majorEventRepo *majorevent.Repository,
	formatter mescheduler.Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepo *delivery.OutboxRepository,
	logger *slog.Logger,
	autoPrepareSchema bool,
) (*mescheduler.Scheduler, *mescheduler.MonthlyScheduler, *mescraper.RuntimeScheduler) {
	majorEventScheduler := mescheduler.NewScheduler(
		majorEventRepo,
		formatter,
		summarizer,
		locker,
		outboxRepo,
		logger,
	)

	majorEventMonthlyScheduler := mescheduler.NewMonthlyScheduler(
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

	majorEventScraperScheduler, err := mescraper.NewRuntimeScheduler(majorEventRepo, logger)
	if err != nil {
		logger.Error("Failed to initialize major event scraper runtime scheduler", slog.String("error", err.Error()))
		majorEventScraperScheduler = nil
	}

	return majorEventScheduler, majorEventMonthlyScheduler, majorEventScraperScheduler
}

func buildMemberNewsComponents(
	memberNews *membernews.Service,
	formatter membernews.DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepo *delivery.OutboxRepository,
	logger *slog.Logger,
) (*mnscheduler.Scheduler, *mnscheduler.MonthlyScheduler) {
	if memberNews == nil {
		logger.Info("Member news scheduler disabled: service unavailable")
		return nil, nil
	}

	scheduler := mnscheduler.NewScheduler(
		memberNews,
		formatter,
		locker,
		outboxRepo,
		logger,
	)
	monthlyScheduler := mnscheduler.NewMonthlyScheduler(
		memberNews,
		formatter,
		locker,
		outboxRepo,
		logger,
	)
	return scheduler, monthlyScheduler
}
