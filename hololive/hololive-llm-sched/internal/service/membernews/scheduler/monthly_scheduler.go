package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const (
	// MonthlyScheduleHourKST: 월간 발송 시각 (KST, runtime 로그에서도 참조)
	MonthlyScheduleHourKST   = 10
	monthlyScheduleMinuteKST = 0

	// MonthlyScheduleDay: 월간 발송 일자 (runtime 로그에서도 참조)
	MonthlyScheduleDay = 1
)

// MonthlyScheduler: 월간 뉴스 자동 발송 스케줄러.
type MonthlyScheduler struct {
	service    model.DigestService
	formatter  DigestFormatter
	locker     delivery.NotificationLocker
	outboxRepo outboxEnqueuer
	logger     *slog.Logger
	now        func() time.Time

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewMonthlyScheduler: 월간 스케줄러 생성.
func NewMonthlyScheduler(
	service model.DigestService,
	formatter DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepo outboxEnqueuer,
	logger *slog.Logger,
) *MonthlyScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}

	return &MonthlyScheduler{
		service:    service,
		formatter:  formatter,
		locker:     locker,
		outboxRepo: outboxRepo,
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

// Start: 월간 발송 루프 시작.
func (s *MonthlyScheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop: 루프 중단.
func (s *MonthlyScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *MonthlyScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	for {
		nextRun := s.calculateNextRun(s.now())
		s.logger.Info("Member news monthly scheduler waiting",
			slog.Time("next_run", nextRun),
			slog.Duration("wait_duration", time.Until(nextRun)),
		)

		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctx.Done():
			timer.Stop()
			s.logger.Info("Member news monthly scheduler stopped by context")
			return
		case <-s.stopCh:
			timer.Stop()
			s.logger.Info("Member news monthly scheduler stopped")
			return
		case <-timer.C:
			if err := s.SendMonthlyDigest(ctx); err != nil {
				s.logger.Error("Member news monthly send failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (s *MonthlyScheduler) calculateNextRun(now time.Time) time.Time {
	nowKST := now.In(kst)

	target := time.Date(
		nowKST.Year(), nowKST.Month(), MonthlyScheduleDay,
		MonthlyScheduleHourKST, monthlyScheduleMinuteKST, 0, 0, kst,
	)

	// 이미 지났으면 다음 달로
	if !target.After(nowKST) {
		target = target.AddDate(0, 1, 0)
	}
	return target
}

// SendMonthlyDigest: 즉시 월간 다이제스트 발송.
func (s *MonthlyScheduler) SendMonthlyDigest(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("member news monthly scheduler is nil")
	}
	if s.service == nil {
		return fmt.Errorf("member news service is nil")
	}

	monthKey := s.getMonthKey()
	lockKey := fmt.Sprintf("membernews:lock:monthly:%s", monthKey)

	// 분산 락 획득
	token, acquired, err := s.locker.TryAcquire(ctx, lockKey, delivery.DefaultExecutionLockTTL)
	if err != nil {
		return fmt.Errorf("acquire monthly execution lock: %w", err)
	}
	if !acquired {
		s.logger.Info("Member news monthly execution skipped: lock already acquired",
			slog.String("month_key", monthKey),
		)
		return nil
	}
	defer func() { _ = s.locker.Release(ctx, lockKey, token) }()

	rooms, err := s.service.ListSubscribedRooms(ctx)
	if err != nil {
		return fmt.Errorf("list subscribed rooms: %w", err)
	}
	if len(rooms) == 0 {
		s.logger.Info("Member news monthly skipped: no subscribed room")
		return nil
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		result delivery.SendResult
		sem    = make(chan struct{}, maxConcurrentDigests)
	)

	for i := range rooms {
		sem <- struct{}{}
		wg.Add(1)
		go func(roomID string) {
			defer wg.Done()
			defer func() { <-sem }()

			roomResult := s.processRoomDigest(ctx, monthKey, roomID)
			mu.Lock()
			result.Merge(roomResult)
			mu.Unlock()
		}(rooms[i].RoomID)
	}
	wg.Wait()

	s.logger.Info("Member news monthly result",
		slog.String("month_key", monthKey),
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("skipped", result.Skipped),
		slog.Int("failed", result.Failed),
		slog.Any("failed_rooms", result.FailedRooms),
	)

	if result.Sent == 0 && result.Failed > 0 {
		return fmt.Errorf("all %d room(s) failed to receive monthly member news", result.Failed)
	}

	return nil
}

// processRoomDigest: 단일 room의 월간 다이제스트 생성 + outbox enqueue.
func (s *MonthlyScheduler) processRoomDigest(ctx context.Context, monthKey, roomID string) delivery.SendResult {
	return processDigestForRoom(ctx, s.service, s.formatter, s.outboxRepo, s.logger,
		PeriodMonthly, domain.DeliveryKindMemberNewsMonthly, monthKey, roomID, "📅 이번달 구독 멤버 뉴스")
}

func (s *MonthlyScheduler) getMonthKey() string {
	now := s.now().In(kst)
	return fmt.Sprintf("%d-%02d", now.Year(), now.Month())
}
