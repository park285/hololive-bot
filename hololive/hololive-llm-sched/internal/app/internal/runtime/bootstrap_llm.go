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

package runtime

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
	majorEventRepository *majorevent.Repository,
	formatter mescheduler.Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepository *delivery.OutboxRepository,
	logger *slog.Logger,
	autoPrepareSchema bool,
) (*mescheduler.Scheduler, *mescheduler.MonthlyScheduler, *mescraper.RuntimeScheduler) {
	majorEventScheduler := mescheduler.NewScheduler(
		majorEventRepository,
		formatter,
		summarizer,
		locker,
		outboxRepository,
		logger,
	)

	majorEventMonthlyScheduler := mescheduler.NewMonthlyScheduler(
		majorEventRepository,
		formatter,
		summarizer,
		locker,
		outboxRepository,
		logger,
	)

	if autoPrepareSchema {
		if err := majorEventRepository.CreateEventsTable(ctx); err != nil {
			logger.Error("Failed to create major_events table", slog.String("error", err.Error()))
		}
	}

	majorEventScraperScheduler, err := mescraper.NewRuntimeScheduler(majorEventRepository, logger)
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
	outboxRepository *delivery.OutboxRepository,
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
		outboxRepository,
		logger,
	)
	monthlyScheduler := mnscheduler.NewMonthlyScheduler(
		memberNews,
		formatter,
		locker,
		outboxRepository,
		logger,
	)
	return scheduler, monthlyScheduler
}
