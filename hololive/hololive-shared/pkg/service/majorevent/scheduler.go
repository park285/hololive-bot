package majorevent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type Formatter interface {
	FormatMajorEventWeeklySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) string
	FormatMajorEventMonthlySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) string
}

// EventRepository: 스케줄러가 사용하는 Repository 메서드 인터페이스
type EventRepository interface {
	GetSubscribedRooms(ctx context.Context) ([]*domain.EventRoomSubscription, error)
	GetEventsByDateRange(ctx context.Context, startDate, endDate time.Time, weekKey string) ([]*domain.MajorEvent, error)
	GetEventsByMonth(ctx context.Context, year, month int, monthKey string) ([]*domain.MajorEvent, error)
	MarkEventsAsNotified(ctx context.Context, eventIDs []int, weekKey string) error
	MarkEventsAsMonthlyNotified(ctx context.Context, eventIDs []int, monthKey string) error
}

type Scheduler struct {
	repository EventRepository
	outboxRepo outboxEnqueuer
	formatter  Formatter
	summarizer *EventSummarizer // nil 허용
	locker     NotificationLocker
	logger     *slog.Logger
	now        func() time.Time

	stopCh   chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func NewScheduler(
	repository EventRepository,
	formatter Formatter,
	summarizer *EventSummarizer,
	locker NotificationLocker,
	outboxRepo outboxEnqueuer,
	logger *slog.Logger,
) *Scheduler {
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}
	return &Scheduler{
		repository: repository,
		outboxRepo: outboxRepo,
		formatter:  formatter,
		summarizer: summarizer,
		locker:     locker,
		logger:     logger,
		now:        time.Now,
		stopCh:     make(chan struct{}),
	}
}

// SetClock: 테스트용 시간 주입.
func (s *Scheduler) SetClock(clockFn func() time.Time) {
	if s == nil || clockFn == nil {
		return
	}
	s.now = clockFn
}

func (s *Scheduler) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()

	for {
		nextRun := s.calculateNextRun(s.clock())
		s.logger.Info("Major event scheduler waiting",
			slog.Time("next_run", nextRun),
			slog.Duration("wait_duration", time.Until(nextRun)))

		timer := time.NewTimer(time.Until(nextRun))

		select {
		case <-ctx.Done():
			timer.Stop()
			s.logger.Info("Scheduler stopped by context")
			return
		case <-s.stopCh:
			timer.Stop()
			s.logger.Info("Scheduler stopped")
			return
		case <-timer.C:
			if err := s.SendWeeklyNotification(ctx); err != nil {
				s.logger.Error("Failed to send weekly notification", slog.String("error", err.Error()))
			}
		}
	}
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
	lockKey := fmt.Sprintf("majorevent:lock:weekly:%s", weekKey)

	// 분산 락 획득
	token, acquired, err := s.locker.TryAcquire(ctx, lockKey, delivery.DefaultExecutionLockTTL)
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	if !acquired {
		return ErrNotificationInProgress
	}
	defer func() { _ = s.locker.Release(ctx, lockKey, token) }()

	rooms, err := s.repository.GetSubscribedRooms(ctx)
	if err != nil {
		return fmt.Errorf("get subscribed rooms: %w", err)
	}

	if len(rooms) == 0 {
		s.logger.Info("No subscribed rooms, skipping notification")
		return nil
	}

	events, err := s.repository.GetEventsByDateRange(ctx, weekStart, weekEnd, weekKey)
	if err != nil {
		return fmt.Errorf("get events from db: %w", err)
	}

	if len(events) == 0 {
		s.logger.Info("No events for this week, skipping notification",
			slog.Time("week_start", weekStart),
			slog.Time("week_end", weekEnd))
		return nil
	}

	domainEvents := make([]domain.MajorEvent, len(events))
	eventIDs := make([]int, len(events))
	for i, e := range events {
		domainEvents[i] = *e
		eventIDs[i] = e.ID
	}

	// LLM 요약 시도 (실패 시 빈 문자열 → template fallback)
	var llmSummary string
	if s.summarizer != nil {
		llmSummary = s.summarizer.Summarize(ctx, domainEvents, SummaryTypeWeekly, weekKey)
	}

	message := s.formatter.FormatMajorEventWeeklySummary(ctx, domainEvents, llmSummary)

	// Room별 outbox enqueue
	targets := make([]roomTarget, len(rooms))
	for i, room := range rooms {
		targets[i] = roomTarget{roomID: room.RoomID}
	}

	result := enqueueToRooms(ctx, s.outboxRepo, targets, domain.DeliveryKindMajorEventWeekly, weekKey, message, s.logger)

	s.logger.Info("Weekly notification enqueue result",
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("failed", result.Failed),
		slog.Int("event_count", len(events)))

	// 마킹 결정표: 전체 성공 → mark, 부분 실패 → no mark, 전체 실패 → error
	switch {
	case result.Sent == 0 && result.Failed > 0:
		return fmt.Errorf("all %d room(s) failed to enqueue notification", result.Failed)
	case result.Failed > 0:
		// 부분 실패 → 마킹 안 함 (다음 실행에서 재시도)
		s.logger.Warn("Partial room enqueue failure, deferring event marking",
			slog.Int("sent", result.Sent),
			slog.Int("failed", result.Failed),
			slog.Any("failed_rooms", result.FailedRooms))
		return nil
	}

	if err := s.repository.MarkEventsAsNotified(ctx, eventIDs, weekKey); err != nil {
		s.logger.Error("Failed to mark events as notified", slog.String("error", err.Error()))
	}

	return nil
}
