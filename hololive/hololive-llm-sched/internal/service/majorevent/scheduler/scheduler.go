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
	digest           *schedulerkit.DigestScheduler
	repository       EventRepository
	outboxRepository outboxEnqueuer
	formatter        Formatter
	summarizer       *mesummarizer.EventSummarizer
}

func NewScheduler(
	repository EventRepository,
	formatter Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
) *Scheduler {
	return &Scheduler{
		digest:           schedulerkit.NewDigestScheduler(locker, logger),
		repository:       repository,
		outboxRepository: outboxRepository,
		formatter:        formatter,
		summarizer:       summarizer,
	}
}

func (s *Scheduler) SetClock(clockFn func() time.Time) {
	if s == nil {
		return
	}
	s.digest.SetClock(clockFn)
}

func (s *Scheduler) Start(ctx context.Context) {
	s.digest.Start(ctx, schedulerkit.Config{
		Logger:           s.digest.Logger,
		WaitingLog:       "Major event scheduler waiting",
		ContextStopLog:   "Scheduler stopped by context",
		StopLog:          "Scheduler stopped",
		CalculateNextRun: s.calculateNextRun,
		OnTick: func(ctx context.Context) {
			if err := s.SendWeeklyNotification(ctx); err != nil {
				s.digest.Logger.Error("Failed to send weekly notification", slog.String("error", err.Error()))
			}
		},
	})
}

func (s *Scheduler) Stop() {
	s.digest.Stop()
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

type weeklyCollected struct {
	rooms  []*domain.EventRoomSubscription
	events []*domain.MajorEvent
}

func (s *Scheduler) SendWeeklyNotification(ctx context.Context) error {
	weekStart, weekEnd := GetWeekRange(s.digest.Clock())
	weekKey := weekStart.Format("2006-01-02")

	return schedulerkit.RunDigest(ctx, s.digest, schedulerkit.DigestOp[weeklyCollected]{
		LockKey:           fmt.Sprintf("majorevent:lock:weekly:%s", weekKey),
		OnLockNotAcquired: func() error { return triggercontracts.ErrNotificationInProgress },
		Collect: func(ctx context.Context) (weeklyCollected, bool, error) {
			return s.weeklyNotificationInputs(ctx, weekStart, weekEnd, weekKey)
		},
		Execute: func(ctx context.Context, c weeklyCollected) error {
			return s.executeWeeklyNotification(ctx, c, weekKey)
		},
	})
}

func (s *Scheduler) weeklyNotificationInputs(
	ctx context.Context,
	weekStart time.Time,
	weekEnd time.Time,
	weekKey string,
) (weeklyCollected, bool, error) {
	rooms, err := s.repository.GetSubscribedRooms(ctx)
	if err != nil {
		return weeklyCollected{}, false, fmt.Errorf("get subscribed rooms: %w", err)
	}
	if len(rooms) == 0 {
		s.digest.Logger.Info("No subscribed rooms, skipping notification")
		return weeklyCollected{}, false, nil
	}

	events, err := s.repository.GetEventsByDateRange(ctx, weekStart, weekEnd, weekKey)
	if err != nil {
		return weeklyCollected{}, false, fmt.Errorf("get events from db: %w", err)
	}
	if len(events) == 0 {
		s.digest.Logger.Info("No events for this week, skipping notification",
			slog.Time("week_start", weekStart),
			slog.Time("week_end", weekEnd))
		return weeklyCollected{}, false, nil
	}
	return weeklyCollected{rooms: rooms, events: events}, true, nil
}

func (s *Scheduler) executeWeeklyNotification(ctx context.Context, c weeklyCollected, weekKey string) error {
	domainEvents, eventIDs := weeklyDomainEvents(c.events)
	message := s.weeklyNotificationMessage(ctx, domainEvents, weekKey)
	result := enqueueToRooms(ctx, s.outboxRepository, roomTargets(c.rooms), domain.DeliveryKindMajorEventWeekly, weekKey, message, s.digest.Logger)

	s.digest.Logger.Info("Weekly notification enqueue result",
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("failed", result.Failed),
		slog.Int("event_count", len(c.events)))

	shouldMark, err := schedulerkit.ShouldMark(result)
	if err != nil {
		return err
	}
	if !shouldMark {
		s.digest.Logger.Warn("Partial room enqueue failure, deferring event marking",
			slog.Int("sent", result.Sent),
			slog.Int("failed", result.Failed),
			slog.Any("failed_rooms", result.FailedRooms))
		return nil
	}
	if err := s.repository.MarkEventsAsNotified(ctx, eventIDs, weekKey); err != nil {
		s.digest.Logger.Error("Failed to mark events as notified", slog.String("error", err.Error()))
	}
	return nil
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
