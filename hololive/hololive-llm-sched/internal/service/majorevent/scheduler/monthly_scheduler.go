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
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/schedulerkit"
	mesummarizer "github.com/kapu/hololive-llm-sched/internal/service/majorevent/summarizer"

	"github.com/kapu/hololive-shared/pkg/constants"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type MonthlyScheduler struct {
	digest           *schedulerkit.DigestScheduler
	repository       EventRepository
	outboxRepository outboxEnqueuer
	formatter        Formatter
	summarizer       *mesummarizer.EventSummarizer
}

func NewMonthlyScheduler(
	repository EventRepository,
	formatter Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
) *MonthlyScheduler {
	return &MonthlyScheduler{
		digest:           schedulerkit.NewDigestScheduler(locker, logger),
		repository:       repository,
		outboxRepository: outboxRepository,
		formatter:        formatter,
		summarizer:       summarizer,
	}
}

func (s *MonthlyScheduler) SetClock(clockFn func() time.Time) {
	if s == nil {
		return
	}
	s.digest.SetClock(clockFn)
}

func (s *MonthlyScheduler) Start(ctx context.Context) {
	s.digest.Start(ctx, &schedulerkit.Config{
		Logger:           s.digest.Logger,
		WaitingLog:       "Monthly event scheduler waiting",
		ContextStopLog:   "Monthly scheduler stopped by context",
		StopLog:          "Monthly scheduler stopped",
		CalculateNextRun: s.calculateNextRun,
		OnTick: func(ctx context.Context) {
			if err := s.SendMonthlyNotification(ctx); err != nil {
				s.digest.Logger.Error("Failed to send monthly notification", slog.String("error", err.Error()))
			}
		},
	})
}

func (s *MonthlyScheduler) Stop() {
	s.digest.Stop()
}

func (s *MonthlyScheduler) calculateNextRun(now time.Time) time.Time {
	nowKST := now.In(kst)

	scheduleHour := constants.MajorEventConfig.MonthlyScheduleHourKST
	scheduleDay := constants.MajorEventConfig.MonthlyScheduleDay

	target := time.Date(
		nowKST.Year(), nowKST.Month(), scheduleDay,
		scheduleHour, 0, 0, 0, kst,
	)

	if !target.After(nowKST) {
		target = target.AddDate(0, 1, 0)
	}

	return target
}

type monthlyCollected struct {
	rooms  []*domain.EventRoomSubscription
	events []*domain.MajorEvent
}

func (s *MonthlyScheduler) SendMonthlyNotification(ctx context.Context) error {
	monthKey := s.getMonthKey()

	return schedulerkit.RunDigest(ctx, s.digest, schedulerkit.DigestOp[monthlyCollected]{
		LockKey:           fmt.Sprintf("majorevent:lock:monthly:%s", monthKey),
		OnLockNotAcquired: func() error { return triggercontracts.ErrNotificationInProgress },
		Collect: func(ctx context.Context) (monthlyCollected, bool, error) {
			return s.monthlyNotificationInputs(ctx, monthKey)
		},
		Execute: func(ctx context.Context, c monthlyCollected) error {
			return s.executeMonthlyNotification(ctx, c, monthKey)
		},
	})
}

func (s *MonthlyScheduler) monthlyNotificationInputs(
	ctx context.Context,
	monthKey string,
) (monthlyCollected, bool, error) {
	rooms, err := s.repository.GetSubscribedRooms(ctx)
	if err != nil {
		return monthlyCollected{}, false, fmt.Errorf("get subscribed rooms: %w", err)
	}
	if len(rooms) == 0 {
		s.digest.Logger.Info("No subscribed rooms, skipping monthly notification")
		return monthlyCollected{}, false, nil
	}

	nowKST := s.digest.Clock().In(kst)
	year, month := nowKST.Year(), int(nowKST.Month())
	events, err := s.repository.GetEventsByMonth(ctx, year, month, monthKey)
	if err != nil {
		return monthlyCollected{}, false, fmt.Errorf("get events by month: %w", err)
	}
	if len(events) == 0 {
		s.digest.Logger.Info("No events for this month, skipping notification",
			slog.Int("year", year),
			slog.Int("month", month))
		return monthlyCollected{}, false, nil
	}
	return monthlyCollected{rooms: rooms, events: events}, true, nil
}

func (s *MonthlyScheduler) executeMonthlyNotification(ctx context.Context, c monthlyCollected, monthKey string) error {
	domainEvents, eventIDs := toDomainEventsAndIDs(c.events)
	message := s.monthlyNotificationMessage(ctx, domainEvents, monthKey)
	result := enqueueToRooms(ctx, s.outboxRepository, roomTargets(c.rooms), domain.DeliveryKindMajorEventMonthly, monthKey, message, s.digest.Logger)

	s.digest.Logger.Info("Monthly notification enqueue result",
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("failed", result.Failed),
		slog.Int("event_count", len(c.events)))

	shouldMark, err := schedulerkit.ShouldMark(result)
	if err != nil {
		return err
	}
	if !shouldMark {
		s.digest.Logger.Warn("Partial room enqueue failure, deferring monthly event marking",
			slog.Int("sent", result.Sent),
			slog.Int("failed", result.Failed),
			slog.Any("failed_rooms", result.FailedRooms))
		return nil
	}
	if err := s.repository.MarkEventsAsMonthlyNotified(ctx, eventIDs, monthKey); err != nil {
		s.digest.Logger.Error("Failed to mark events as monthly notified", slog.String("error", err.Error()))
	}
	return nil
}

func (s *MonthlyScheduler) monthlyNotificationMessage(ctx context.Context, events []domain.MajorEvent, monthKey string) string {
	var llmSummary string
	if s.summarizer != nil {
		llmSummary = s.summarizer.Summarize(ctx, events, mesummarizer.SummaryTypeMonthly, monthKey)
	}
	return s.formatter.FormatMajorEventMonthlySummary(ctx, events, llmSummary)
}

func (s *MonthlyScheduler) getMonthKey() string {
	now := s.digest.Clock().In(kst)
	return fmt.Sprintf("%d-%02d", now.Year(), now.Month())
}
