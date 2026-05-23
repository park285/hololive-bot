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
	repository       EventRepository
	outboxRepository outboxEnqueuer
	formatter        Formatter
	summarizer       *mesummarizer.EventSummarizer
	locker           delivery.NotificationLocker
	logger           *slog.Logger
	runtime          *schedulerkit.Runtime
}

func NewMonthlyScheduler(
	repository EventRepository,
	formatter Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
) *MonthlyScheduler {
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}
	return &MonthlyScheduler{
		repository:       repository,
		outboxRepository: outboxRepository,
		formatter:        formatter,
		summarizer:       summarizer,
		locker:           locker,
		logger:           logger,
		runtime:          schedulerkit.NewRuntime(),
	}
}

func (s *MonthlyScheduler) SetClock(clockFn func() time.Time) {
	if s == nil {
		return
	}
	s.runtime.SetClock(clockFn)
}

func (s *MonthlyScheduler) clock() time.Time {
	if s == nil || s.runtime == nil {
		return time.Now()
	}
	return s.runtime.Now()
}

func (s *MonthlyScheduler) Start(ctx context.Context) {
	s.runtime.Start(ctx, schedulerkit.Config{
		Logger:           s.logger,
		WaitingLog:       "Monthly event scheduler waiting",
		ContextStopLog:   "Monthly scheduler stopped by context",
		StopLog:          "Monthly scheduler stopped",
		CalculateNextRun: s.calculateNextRun,
		OnTick: func(ctx context.Context) {
			if err := s.SendMonthlyNotification(ctx); err != nil {
				s.logger.Error("Failed to send monthly notification", slog.String("error", err.Error()))
			}
		},
	})
}

func (s *MonthlyScheduler) Stop() {
	s.runtime.Stop()
}

func (s *MonthlyScheduler) calculateNextRun(now time.Time) time.Time {
	nowKST := now.In(kst)

	scheduleHour := constants.MajorEventConfig.MonthlyScheduleHourKST
	scheduleDay := constants.MajorEventConfig.MonthlyScheduleDay

	// 이번 달 scheduleDay일 scheduleHour시
	target := time.Date(
		nowKST.Year(), nowKST.Month(), scheduleDay,
		scheduleHour, 0, 0, 0, kst,
	)

	// 이미 지났으면 다음 달로
	if !target.After(nowKST) {
		target = target.AddDate(0, 1, 0)
	}

	return target
}

func (s *MonthlyScheduler) SendMonthlyNotification(ctx context.Context) error {
	monthKey := s.getMonthKey()
	releaseLock, err := s.acquireMonthlyNotificationLock(ctx, monthKey)
	if err != nil {
		return err
	}
	defer releaseLock()

	rooms, events, ok, err := s.monthlyNotificationInputs(ctx, monthKey)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	eventIDs, shouldMark, err := s.enqueueMonthlyNotification(ctx, rooms, events, monthKey)
	if err != nil || !shouldMark {
		return err
	}
	if err := s.repository.MarkEventsAsMonthlyNotified(ctx, eventIDs, monthKey); err != nil {
		s.logger.Error("Failed to mark events as monthly notified", slog.String("error", err.Error()))
	}

	return nil
}

func (s *MonthlyScheduler) acquireMonthlyNotificationLock(ctx context.Context, monthKey string) (func(), error) {
	lockKey := fmt.Sprintf("majorevent:lock:monthly:%s", monthKey)
	token, acquired, err := s.locker.TryAcquire(ctx, lockKey, delivery.DefaultExecutionLockTTL)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !acquired {
		return nil, triggercontracts.ErrNotificationInProgress
	}
	return func() { _ = s.locker.Release(ctx, lockKey, token) }, nil
}

func (s *MonthlyScheduler) monthlyNotificationInputs(
	ctx context.Context,
	monthKey string,
) ([]*domain.EventRoomSubscription, []*domain.MajorEvent, bool, error) {
	rooms, err := s.repository.GetSubscribedRooms(ctx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("get subscribed rooms: %w", err)
	}
	if len(rooms) == 0 {
		s.logger.Info("No subscribed rooms, skipping monthly notification")
		return nil, nil, false, nil
	}

	nowKST := s.clock().In(kst)
	year, month := nowKST.Year(), int(nowKST.Month())
	events, err := s.repository.GetEventsByMonth(ctx, year, month, monthKey)
	if err != nil {
		return nil, nil, false, fmt.Errorf("get events by month: %w", err)
	}
	if len(events) == 0 {
		s.logger.Info("No events for this month, skipping notification",
			slog.Int("year", year),
			slog.Int("month", month))
		return nil, nil, false, nil
	}
	return rooms, events, true, nil
}

func (s *MonthlyScheduler) enqueueMonthlyNotification(
	ctx context.Context,
	rooms []*domain.EventRoomSubscription,
	events []*domain.MajorEvent,
	monthKey string,
) ([]int, bool, error) {
	domainEvents, eventIDs := toDomainEventsAndIDs(events)
	message := s.monthlyNotificationMessage(ctx, domainEvents, monthKey)
	result := enqueueToRooms(ctx, s.outboxRepository, toRoomTargets(rooms), domain.DeliveryKindMajorEventMonthly, monthKey, message, s.logger)
	s.logMonthlyNotificationEnqueueResult(result, len(events))
	shouldMark, err := s.shouldMarkMonthlyEvents(result)
	return eventIDs, shouldMark, err
}

func (s *MonthlyScheduler) monthlyNotificationMessage(ctx context.Context, events []domain.MajorEvent, monthKey string) string {
	var llmSummary string
	if s.summarizer != nil {
		llmSummary = s.summarizer.Summarize(ctx, events, mesummarizer.SummaryTypeMonthly, monthKey)
	}
	return s.formatter.FormatMajorEventMonthlySummary(ctx, events, llmSummary)
}

func (s *MonthlyScheduler) logMonthlyNotificationEnqueueResult(result delivery.SendResult, eventCount int) {
	s.logger.Info("Monthly notification enqueue result",
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("failed", result.Failed),
		slog.Int("event_count", eventCount))
}

func (s *MonthlyScheduler) shouldMarkMonthlyEvents(result delivery.SendResult) (bool, error) {
	switch {
	case result.Sent == 0 && result.Failed > 0:
		return false, fmt.Errorf("all %d room(s) failed to enqueue monthly notification", result.Failed)
	case result.Failed > 0:
		s.logger.Warn("Partial room enqueue failure, deferring monthly event marking",
			slog.Int("sent", result.Sent),
			slog.Int("failed", result.Failed),
			slog.Any("failed_rooms", result.FailedRooms))
		return false, nil
	default:
		return true, nil
	}
}

func toDomainEventsAndIDs(events []*domain.MajorEvent) ([]domain.MajorEvent, []int) {
	domainEvents := make([]domain.MajorEvent, len(events))
	eventIDs := make([]int, len(events))
	for i, e := range events {
		domainEvents[i] = *e
		eventIDs[i] = e.ID
	}
	return domainEvents, eventIDs
}

func toRoomTargets(rooms []*domain.EventRoomSubscription) []roomTarget {
	targets := make([]roomTarget, len(rooms))
	for i, room := range rooms {
		targets[i] = roomTarget{roomID: room.RoomID}
	}
	return targets
}

func (s *MonthlyScheduler) getMonthKey() string {
	now := s.clock().In(kst)
	return fmt.Sprintf("%d-%02d", now.Year(), now.Month())
}
