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
	"log/slog"

	"github.com/park285/shared-go/pkg/outputguard"
	"github.com/park285/shared-go/pkg/promptguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent"
	mescheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scheduler"
	mescraper "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scraper"
	mesummarizer "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/summarizer"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	mnscheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/scheduler"

	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

func buildMajorEventComponents(
	majorEventRepository *majorevent.Repository,
	formatter mescheduler.Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepository *delivery.OutboxRepository,
	guards *llmGuards,
	logger *slog.Logger,
) (*mescheduler.Scheduler, *mescheduler.MonthlyScheduler, *mescraper.RuntimeScheduler) {
	var promptGuard *promptguard.Guard
	var outputGuard *outputguard.Guard
	if guards != nil {
		promptGuard = guards.prompt
		outputGuard = guards.output
	}

	majorEventScheduler := mescheduler.NewScheduler(
		majorEventRepository,
		formatter,
		summarizer,
		locker,
		outboxRepository,
		logger,
		mescheduler.WithGuards(promptGuard, outputGuard),
	)

	majorEventMonthlyScheduler := mescheduler.NewMonthlyScheduler(
		majorEventRepository,
		formatter,
		summarizer,
		locker,
		outboxRepository,
		logger,
		mescheduler.WithMonthlyGuards(promptGuard, outputGuard),
	)

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
	outputGuard *outputguard.Guard,
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
		mnscheduler.WithOutputGuard(outputGuard),
	)
	monthlyScheduler := mnscheduler.NewMonthlyScheduler(
		memberNews,
		formatter,
		locker,
		outboxRepository,
		logger,
		mnscheduler.WithMonthlyOutputGuard(outputGuard),
	)
	return scheduler, monthlyScheduler
}
