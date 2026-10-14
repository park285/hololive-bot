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

	"github.com/park285/shared-go/v2/pkg/outputguard"
	"github.com/park285/shared-go/v2/pkg/promptguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/schedulerkit"
	mesummarizer "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/summarizer"
	"github.com/kapu/hololive-shared/pkg/constants"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

var kst = timeutil.KSTZone

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

// Formatter의 렌더 실패는 오류다. 스케줄러는 실패한 본문을 enqueue하지 않고 이벤트를 미표시로 남긴다.
type Formatter interface {
	FormatMajorEventWeeklySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) (string, error)
	FormatMajorEventMonthlySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) (string, error)
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
	promptGuard      *promptguard.Guard
	outputGuard      *outputguard.Guard
}

type SchedulerOption func(*Scheduler)

func WithGuards(promptGuard *promptguard.Guard, outputGuard *outputguard.Guard) SchedulerOption {
	return func(scheduler *Scheduler) {
		scheduler.promptGuard = promptGuard
		scheduler.outputGuard = outputGuard
	}
}

func NewScheduler(
	repository EventRepository,
	formatter Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepository outboxEnqueuer,
	logger *slog.Logger,
	opts ...SchedulerOption,
) *Scheduler {
	scheduler := &Scheduler{
		digest:           schedulerkit.NewDigestScheduler(locker, logger),
		repository:       repository,
		outboxRepository: outboxRepository,
		formatter:        formatter,
		summarizer:       summarizer,
	}

	for _, opt := range opts {
		opt(scheduler)
	}

	return scheduler
}

func (s *Scheduler) SetClock(clockFn func() time.Time) {
	if s == nil {
		return
	}

	s.digest.SetClock(clockFn)
}

func (s *Scheduler) Start(ctx context.Context) {
	s.digest.Start(ctx, &schedulerkit.Config{
		Logger:           s.digest.Logger,
		WaitingLog:       "Major event scheduler waiting",
		ContextStopLog:   "Scheduler stopped by context",
		StopLog:          "Scheduler stopped",
		CalculateNextRun: s.calculateNextRun,
		OnTick: func(ctx context.Context) {
			if _, err := s.SendWeeklyNotification(ctx); err != nil {
				s.digest.Logger.Error("Failed to send weekly notification", slog.Any("error", err))
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

func (s *Scheduler) SendWeeklyNotification(ctx context.Context) (triggercontracts.Result, error) {
	weekStart, weekEnd := GetWeekRange(s.digest.Clock())
	weekKey := weekStart.Format(time.DateOnly)
	result := triggercontracts.NewResult(0, 0, 0)

	if err := s.digest.RunDigest(ctx, schedulerkit.DigestOp[weeklyCollected]{
		LockKey:           fmt.Sprintf("majorevent:lock:weekly:%s", weekKey),
		OnLockNotAcquired: func() error { return triggercontracts.ErrNotificationInProgress },
		Collect: func(ctx context.Context) (weeklyCollected, bool, error) {
			return s.weeklyNotificationInputs(ctx, weekStart, weekEnd, weekKey)
		},
		Execute: func(ctx context.Context, c weeklyCollected) error {
			var err error

			result, err = s.executeWeeklyNotification(ctx, c, weekKey)

			return err
		},
	}); err != nil {
		return result, fmt.Errorf("run digest: %w", err)
	}

	return result, nil
}

func (s *Scheduler) weeklyNotificationInputs(
	ctx context.Context,
	weekStart time.Time,
	weekEnd time.Time,
	weekKey string,
) (weeklyCollected, bool, error) {
	rooms, err := s.repository.GetSubscribedRooms(ctx)
	if err != nil {
		return weeklyCollected{}, false, fmt.Errorf("collect weekly subscribed rooms: %w", err)
	}

	if len(rooms) == 0 {
		s.digest.Logger.Info("No subscribed rooms, skipping notification")

		return weeklyCollected{}, false, nil
	}

	events, err := s.repository.GetEventsByDateRange(ctx, weekStart, weekEnd, weekKey)
	if err != nil {
		return weeklyCollected{}, false, fmt.Errorf("get events from db: %w", err)
	}

	events, err = filterPromptEvents(events, s.promptGuard, s.digest.Logger)
	if err != nil {
		return weeklyCollected{}, false, fmt.Errorf("guard major events: %w", err)
	}

	if len(events) == 0 {
		s.digest.Logger.Info("No events for this week, skipping notification",
			slog.Time("week_start", weekStart),
			slog.Time("week_end", weekEnd))

		return weeklyCollected{}, false, nil
	}

	return weeklyCollected{rooms: rooms, events: events}, true, nil
}

func (s *Scheduler) executeWeeklyNotification(ctx context.Context, c weeklyCollected, weekKey string) (triggercontracts.Result, error) {
	return executePeriodNotification(ctx, periodNotification{
		outboxRepository: s.outboxRepository,
		outputGuard:      s.outputGuard,
		logger:           s.digest.Logger,
		rooms:            c.rooms,
		events:           c.events,
		periodKey:        weekKey,
		kind:             domain.DeliveryKindMajorEventWeekly,
		message:          s.weeklyNotificationMessage,
		mark:             s.repository.MarkEventsAsNotified,
		errorPrefix:      "execute weekly notification",
		enqueueLogMsg:    "Weekly notification enqueue result",
		deferLogMsg:      "Partial room enqueue failure, deferring event marking",
		markErrorLog:     "Failed to mark events as notified",
	})
}

// periodNotification은 주간·월간 주요 일정 알림이 공유하는 실행 흐름(메시지 생성, 방별 enqueue, 이벤트 표시)의 차이점이다.
type periodNotification struct {
	outboxRepository outboxEnqueuer
	outputGuard      *outputguard.Guard
	logger           *slog.Logger
	rooms            []*domain.EventRoomSubscription
	events           []*domain.MajorEvent
	periodKey        string
	kind             domain.DeliveryOutboxKind
	message          func(ctx context.Context, events []domain.MajorEvent, periodKey string) (string, error)
	mark             func(ctx context.Context, eventIDs []int, periodKey string) error
	errorPrefix      string
	enqueueLogMsg    string
	deferLogMsg      string
	markErrorLog     string
}

// executePeriodNotification은 enqueue 결과를 Result로 돌려주고, 일부 방 enqueue가 실패하면 이벤트 표시를 미룬다.
// 표시 실패는 이미 enqueue된 알림을 되돌릴 수 없으므로 오류 대신 로그로만 남긴다.
func executePeriodNotification(ctx context.Context, n periodNotification) (triggercontracts.Result, error) {
	domainEvents, eventIDs := toDomainEventsAndIDs(n.events)

	message, err := n.message(ctx, domainEvents, n.periodKey)
	if err != nil {
		return triggercontracts.Result{}, fmt.Errorf("%s: format message: %w", n.errorPrefix, err)
	}

	counts, shouldMark, err := enqueueNotification(ctx, notificationEnqueue{
		outboxRepository: n.outboxRepository,
		outputGuard:      n.outputGuard,
		logger:           n.logger,
		rooms:            n.rooms,
		kind:             n.kind,
		periodKey:        n.periodKey,
		message:          message,
		eventCount:       len(n.events),
		enqueueLogMsg:    n.enqueueLogMsg,
		deferLogMsg:      n.deferLogMsg,
	})
	result := triggercontracts.NewResult(counts.Sent, counts.Skipped, counts.Failed)

	if err != nil {
		return result, fmt.Errorf("%s: %w", n.errorPrefix, err)
	}

	if !shouldMark {
		return result, nil
	}

	if err := n.mark(ctx, eventIDs, n.periodKey); err != nil {
		n.logger.Error(n.markErrorLog, slog.Any("error", err))
	}

	return result, nil
}

func toDomainEventsAndIDs(events []*domain.MajorEvent) (domainEvents []domain.MajorEvent, eventIDs []int) {
	domainEvents = make([]domain.MajorEvent, len(events))
	eventIDs = make([]int, len(events))

	for i, e := range events {
		domainEvents[i] = *e
		eventIDs[i] = e.ID
	}

	return domainEvents, eventIDs
}

func (s *Scheduler) weeklyNotificationMessage(ctx context.Context, events []domain.MajorEvent, weekKey string) (string, error) {
	var llmSummary string

	// 요약 실패는 이벤트 목록만 보내는 대체 경로로 바꾸지 않고 오류로 돌려준다. 이벤트는 미표시로 남는다.
	if s.summarizer != nil {
		summary, err := s.summarizer.Summarize(ctx, events, mesummarizer.SummaryTypeWeekly, weekKey)
		if err != nil {
			return "", fmt.Errorf("summarize weekly events: %w", err)
		}

		llmSummary = summary
	}

	message, err := s.formatter.FormatMajorEventWeeklySummary(ctx, events, llmSummary)
	if err != nil {
		return "", fmt.Errorf("format weekly summary: %w", err)
	}

	return message, nil
}

func roomTargets(rooms []*domain.EventRoomSubscription) []roomTarget {
	targets := make([]roomTarget, len(rooms))
	for i, room := range rooms {
		targets[i] = roomTarget{roomID: room.RoomID}
	}

	return targets
}
