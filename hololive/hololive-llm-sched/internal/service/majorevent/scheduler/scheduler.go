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

// kst: 한국 표준시 (UTC+9)
var kst = time.FixedZone("KST", 9*60*60)

// 월요일 발송 기준: 발송 당일(월)부터 일요일까지의 이벤트를 대상으로 합니다.
func GetWeekRange(now time.Time) (start, end time.Time) {
	nowKST := now.In(kst)

	// 이번 주 월요일 찾기 (월요일이면 당일)
	daysFromMonday := (int(nowKST.Weekday()) - int(time.Monday) + 7) % 7
	monday := time.Date(
		nowKST.Year(), nowKST.Month(), nowKST.Day()-daysFromMonday,
		0, 0, 0, 0, kst,
	)

	// 같은 주 일요일 23:59:59
	sunday := monday.AddDate(0, 0, 6)
	sundayEnd := time.Date(
		sunday.Year(), sunday.Month(), sunday.Day(),
		23, 59, 59, 0, kst,
	)

	return monday, sundayEnd
}

type Formatter interface {
	FormatMajorEventWeeklySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) string
	FormatMajorEventMonthlySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) string
}

type EventRepository interface {
	GetSubscribedRooms(ctx context.Context) ([]*domain.EventRoomSubscription, error)
	GetEventsByDateRange(ctx context.Context, startDate, endDate time.Time, weekKey string) ([]*domain.MajorEvent, error)
	GetEventsByMonth(ctx context.Context, year, month int, monthKey string) ([]*domain.MajorEvent, error)
	MarkEventsAsNotified(ctx context.Context, eventIDs []int, weekKey string) error
	MarkEventsAsMonthlyNotified(ctx context.Context, eventIDs []int, monthKey string) error
}

type Scheduler struct {
	repository       EventRepository
	outboxRepository outboxEnqueuer
	formatter        Formatter
	summarizer       *mesummarizer.EventSummarizer // nil 허용
	locker           delivery.NotificationLocker
	logger           *slog.Logger
	runtime          *schedulerkit.Runtime
}

func NewScheduler(
	repository EventRepository,
	formatter Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
) *Scheduler {
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}
	return &Scheduler{
		repository:       repository,
		outboxRepository: outboxRepository,
		formatter:        formatter,
		summarizer:       summarizer,
		locker:           locker,
		logger:           logger,
		runtime:          schedulerkit.NewRuntime(),
	}
}

func (s *Scheduler) SetClock(clockFn func() time.Time) {
	if s == nil {
		return
	}
	s.runtime.SetClock(clockFn)
}

func (s *Scheduler) clock() time.Time {
	if s == nil || s.runtime == nil {
		return time.Now()
	}
	return s.runtime.Now()
}

func (s *Scheduler) Start(ctx context.Context) {
	s.runtime.Start(ctx, schedulerkit.Config{
		Logger:           s.logger,
		WaitingLog:       "Major event scheduler waiting",
		ContextStopLog:   "Scheduler stopped by context",
		StopLog:          "Scheduler stopped",
		CalculateNextRun: s.calculateNextRun,
		OnTick: func(ctx context.Context) {
			if err := s.SendWeeklyNotification(ctx); err != nil {
				s.logger.Error("Failed to send weekly notification", slog.String("error", err.Error()))
			}
		},
	})
}

func (s *Scheduler) Stop() {
	s.runtime.Stop()
}

func (s *Scheduler) calculateNextRun(now time.Time) time.Time {
	nowKST := now.In(kst)

	scheduleHour := constants.MajorEventConfig.ScheduleHourKST
	scheduleWeekday := constants.MajorEventConfig.ScheduleWeekday

	daysUntilTarget := (int(scheduleWeekday) - int(nowKST.Weekday()) + 7) % 7

	targetDate := time.Date(
		nowKST.Year(), nowKST.Month(), nowKST.Day()+daysUntilTarget,
		scheduleHour, 0, 0, 0, kst,
	)

	if !targetDate.After(nowKST) {
		targetDate = targetDate.AddDate(0, 0, 7)
	}

	return targetDate
}

func (s *Scheduler) SendWeeklyNotification(ctx context.Context) error {
	weekStart, weekEnd := GetWeekRange(s.clock())
	weekKey := weekStart.Format("2006-01-02")
	releaseLock, err := s.acquireWeeklyNotificationLock(ctx, weekKey)
	if err != nil {
		return err
	}
	defer releaseLock()

	rooms, events, ok, err := s.weeklyNotificationInputs(ctx, weekStart, weekEnd, weekKey)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	eventIDs, shouldMark, err := s.enqueueWeeklyNotification(ctx, rooms, events, weekKey)
	if err != nil || !shouldMark {
		return err
	}
	if err := s.repository.MarkEventsAsNotified(ctx, eventIDs, weekKey); err != nil {
		s.logger.Error("Failed to mark events as notified", slog.String("error", err.Error()))
	}

	return nil
}

func (s *Scheduler) weeklyNotificationInputs(
	ctx context.Context,
	weekStart time.Time,
	weekEnd time.Time,
	weekKey string,
) ([]*domain.EventRoomSubscription, []*domain.MajorEvent, bool, error) {
	rooms, err := s.repository.GetSubscribedRooms(ctx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("get subscribed rooms: %w", err)
	}
	if len(rooms) == 0 {
		s.logger.Info("No subscribed rooms, skipping notification")
		return nil, nil, false, nil
	}

	events, err := s.repository.GetEventsByDateRange(ctx, weekStart, weekEnd, weekKey)
	if err != nil {
		return nil, nil, false, fmt.Errorf("get events from db: %w", err)
	}
	if len(events) == 0 {
		s.logger.Info("No events for this week, skipping notification",
			slog.Time("week_start", weekStart),
			slog.Time("week_end", weekEnd))
		return nil, nil, false, nil
	}
	return rooms, events, true, nil
}

func (s *Scheduler) enqueueWeeklyNotification(
	ctx context.Context,
	rooms []*domain.EventRoomSubscription,
	events []*domain.MajorEvent,
	weekKey string,
) ([]int, bool, error) {
	domainEvents, eventIDs := weeklyDomainEvents(events)
	message := s.weeklyNotificationMessage(ctx, domainEvents, weekKey)
	result := enqueueToRooms(ctx, s.outboxRepository, roomTargets(rooms), domain.DeliveryKindMajorEventWeekly, weekKey, message, s.logger)
	s.logWeeklyNotificationEnqueueResult(result, len(events))
	shouldMark, err := s.shouldMarkWeeklyEvents(result)
	return eventIDs, shouldMark, err
}

func (s *Scheduler) logWeeklyNotificationEnqueueResult(result delivery.SendResult, eventCount int) {
	s.logger.Info("Weekly notification enqueue result",
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("failed", result.Failed),
		slog.Int("event_count", eventCount))
}

func (s *Scheduler) acquireWeeklyNotificationLock(ctx context.Context, weekKey string) (func(), error) {
	lockKey := fmt.Sprintf("majorevent:lock:weekly:%s", weekKey)
	token, acquired, err := s.locker.TryAcquire(ctx, lockKey, delivery.DefaultExecutionLockTTL)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !acquired {
		return nil, triggercontracts.ErrNotificationInProgress
	}
	return func() { _ = s.locker.Release(ctx, lockKey, token) }, nil
}

func weeklyDomainEvents(events []*domain.MajorEvent) ([]domain.MajorEvent, []int) {
	domainEvents := make([]domain.MajorEvent, len(events))
	eventIDs := make([]int, len(events))
	for i, e := range events {
		domainEvents[i] = *e
		eventIDs[i] = e.ID
	}
	return domainEvents, eventIDs
}

func (s *Scheduler) weeklyNotificationMessage(ctx context.Context, events []domain.MajorEvent, weekKey string) string {
	var llmSummary string
	if s.summarizer != nil {
		llmSummary = s.summarizer.Summarize(ctx, events, mesummarizer.SummaryTypeWeekly, weekKey)
	}
	return s.formatter.FormatMajorEventWeeklySummary(ctx, events, llmSummary)
}

func roomTargets(rooms []*domain.EventRoomSubscription) []roomTarget {
	targets := make([]roomTarget, len(rooms))
	for i, room := range rooms {
		targets[i] = roomTarget{roomID: room.RoomID}
	}
	return targets
}

func (s *Scheduler) shouldMarkWeeklyEvents(result delivery.SendResult) (bool, error) {
	switch {
	case result.Sent == 0 && result.Failed > 0:
		return false, fmt.Errorf("all %d room(s) failed to enqueue notification", result.Failed)
	case result.Failed > 0:
		s.logger.Warn("Partial room enqueue failure, deferring event marking",
			slog.Int("sent", result.Sent),
			slog.Int("failed", result.Failed),
			slog.Any("failed_rooms", result.FailedRooms))
		return false, nil
	default:
		return true, nil
	}
}
