package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mesummarizer "github.com/kapu/hololive-llm-sched/internal/service/majorevent/summarizer"

	"github.com/kapu/hololive-shared/pkg/constants"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

// MonthlyScheduler: 매월 1일에 월간 행사 요약을 발송하는 스케줄러
type MonthlyScheduler struct {
	repository EventRepository
	outboxRepo outboxEnqueuer
	formatter  Formatter
	summarizer *mesummarizer.EventSummarizer
	locker     delivery.NotificationLocker
	logger     *slog.Logger
	now        func() time.Time

	stopCh   chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// NewMonthlyScheduler: 월간 스케줄러를 생성합니다.
func NewMonthlyScheduler(
	repository EventRepository,
	formatter Formatter,
	summarizer *mesummarizer.EventSummarizer,
	locker delivery.NotificationLocker,
	outboxRepo outboxEnqueuer,
	logger *slog.Logger,
) *MonthlyScheduler {
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}
	return &MonthlyScheduler{
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
func (s *MonthlyScheduler) SetClock(clockFn func() time.Time) {
	if s == nil || clockFn == nil {
		return
	}
	s.now = clockFn
}

func (s *MonthlyScheduler) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Start: 월간 스케줄러를 시작합니다.
func (s *MonthlyScheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop: 월간 스케줄러를 중지합니다.
func (s *MonthlyScheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *MonthlyScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	for {
		nextRun := s.calculateNextRun(s.clock())
		s.logger.Info("Monthly event scheduler waiting",
			slog.Time("next_run", nextRun),
			slog.Duration("wait_duration", time.Until(nextRun)))

		timer := time.NewTimer(time.Until(nextRun))

		select {
		case <-ctx.Done():
			timer.Stop()
			s.logger.Info("Monthly scheduler stopped by context")
			return
		case <-s.stopCh:
			timer.Stop()
			s.logger.Info("Monthly scheduler stopped")
			return
		case <-timer.C:
			if err := s.SendMonthlyNotification(ctx); err != nil {
				s.logger.Error("Failed to send monthly notification", slog.String("error", err.Error()))
			}
		}
	}
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

// SendMonthlyNotification: 월간 행사 알림을 발송합니다.
func (s *MonthlyScheduler) SendMonthlyNotification(ctx context.Context) error {
	monthKey := s.getMonthKey()
	lockKey := fmt.Sprintf("majorevent:lock:monthly:%s", monthKey)

	// 분산 락 획득
	token, acquired, err := s.locker.TryAcquire(ctx, lockKey, delivery.DefaultExecutionLockTTL)
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	if !acquired {
		return triggercontracts.ErrNotificationInProgress
	}
	defer func() { _ = s.locker.Release(ctx, lockKey, token) }()

	rooms, err := s.repository.GetSubscribedRooms(ctx)
	if err != nil {
		return fmt.Errorf("get subscribed rooms: %w", err)
	}

	if len(rooms) == 0 {
		s.logger.Info("No subscribed rooms, skipping monthly notification")
		return nil
	}

	nowKST := s.clock().In(kst)
	year, month := nowKST.Year(), int(nowKST.Month())

	events, err := s.repository.GetEventsByMonth(ctx, year, month, monthKey)
	if err != nil {
		return fmt.Errorf("get events by month: %w", err)
	}

	if len(events) == 0 {
		s.logger.Info("No events for this month, skipping notification",
			slog.Int("year", year),
			slog.Int("month", month))
		return nil
	}

	domainEvents, eventIDs := toDomainEventsAndIDs(events)

	// LLM 요약 시도
	var llmSummary string
	if s.summarizer != nil {
		llmSummary = s.summarizer.Summarize(ctx, domainEvents, mesummarizer.SummaryTypeMonthly, monthKey)
	}

	message := s.formatter.FormatMajorEventMonthlySummary(ctx, domainEvents, llmSummary)

	// Room별 outbox enqueue
	targets := toRoomTargets(rooms)

	result := enqueueToRooms(ctx, s.outboxRepo, targets, domain.DeliveryKindMajorEventMonthly, monthKey, message, s.logger)

	s.logger.Info("Monthly notification enqueue result",
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("failed", result.Failed),
		slog.Int("event_count", len(events)))

	// 마킹 결정표
	switch {
	case result.Sent == 0 && result.Failed > 0:
		return fmt.Errorf("all %d room(s) failed to enqueue monthly notification", result.Failed)
	case result.Failed > 0:
		s.logger.Warn("Partial room enqueue failure, deferring monthly event marking",
			slog.Int("sent", result.Sent),
			slog.Int("failed", result.Failed),
			slog.Any("failed_rooms", result.FailedRooms))
		return nil
	}

	if err := s.repository.MarkEventsAsMonthlyNotified(ctx, eventIDs, monthKey); err != nil {
		s.logger.Error("Failed to mark events as monthly notified", slog.String("error", err.Error()))
	}

	return nil
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
