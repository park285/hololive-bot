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

const maxConcurrentDigests = 5

const (
	// WeeklyScheduleWeekday: 주간 발송 요일 (runtime 로그에서도 참조)
	WeeklyScheduleWeekday = time.Monday

	// WeeklyScheduleHourKST: 주간 발송 시각 (KST, runtime 로그에서도 참조)
	WeeklyScheduleHourKST   = 9
	weeklyScheduleMinuteKST = 0
)

var (
	kst                    = model.KST
	ErrNoSubscribedMembers = model.ErrNoSubscribedMembers
)

const (
	PeriodWeekly  = model.PeriodWeekly
	PeriodMonthly = model.PeriodMonthly
)

type (
	Period          = model.Period
	Digest          = model.Digest
	SummaryItem     = model.SummaryItem
	SubscribedRoom  = model.SubscribedRoom
	DigestFormatter = model.DigestFormatter
	digestService   = model.DigestService
)

// outboxEnqueuer: outbox enqueue 연산 인터페이스 (테스트 mock 용도)
type outboxEnqueuer interface {
	Enqueue(ctx context.Context, kind domain.DeliveryOutboxKind, periodKey, roomID, message string) error
}

// Scheduler: 주간 뉴스 자동 발송 스케줄러.
type Scheduler struct {
	service    digestService
	formatter  DigestFormatter
	locker     delivery.NotificationLocker
	outboxRepo outboxEnqueuer
	logger     *slog.Logger
	now        func() time.Time

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewScheduler: 스케줄러 생성.
func NewScheduler(
	service digestService,
	formatter DigestFormatter,
	locker delivery.NotificationLocker,
	outboxRepo outboxEnqueuer,
	logger *slog.Logger,
) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if locker == nil {
		locker = delivery.NewLocker(nil, logger)
	}

	return &Scheduler{
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
func (s *Scheduler) SetClock(clockFn func() time.Time) {
	if s == nil || clockFn == nil {
		return
	}
	s.now = clockFn
}

// Start: 주간 발송 루프 시작.
func (s *Scheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.wg.Add(1)
	go s.run(ctx)
}

// Stop: 루프 중단.
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()

	for {
		nextRun := s.calculateNextRun(s.now())
		s.logger.Info("Member news scheduler waiting",
			slog.Time("next_run", nextRun),
			slog.Duration("wait_duration", time.Until(nextRun)),
		)

		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctx.Done():
			timer.Stop()
			s.logger.Info("Member news scheduler stopped by context")
			return
		case <-s.stopCh:
			timer.Stop()
			s.logger.Info("Member news scheduler stopped")
			return
		case <-timer.C:
			if err := s.SendWeeklyDigest(ctx); err != nil {
				s.logger.Error("Member news weekly send failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (s *Scheduler) calculateNextRun(now time.Time) time.Time {
	nowKST := now.In(kst)

	daysUntilMonday := (int(time.Monday) - int(nowKST.Weekday()) + 7) % 7
	target := time.Date(
		nowKST.Year(), nowKST.Month(), nowKST.Day()+daysUntilMonday,
		WeeklyScheduleHourKST, weeklyScheduleMinuteKST, 0, 0, kst,
	)

	if !target.After(nowKST) {
		target = target.AddDate(0, 0, 7)
	}
	return target
}

// SendWeeklyDigest: 즉시 주간 다이제스트 발송.
func (s *Scheduler) SendWeeklyDigest(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("member news scheduler is nil")
	}
	if s.service == nil {
		return fmt.Errorf("member news service is nil")
	}

	weekKey := startOfWeek(s.now()).Format("2006-01-02")
	lockKey := fmt.Sprintf("membernews:lock:weekly:%s", weekKey)

	// 분산 락 획득
	token, acquired, err := s.locker.TryAcquire(ctx, lockKey, delivery.DefaultExecutionLockTTL)
	if err != nil {
		return fmt.Errorf("acquire weekly execution lock: %w", err)
	}
	if !acquired {
		s.logger.Info("Member news weekly execution skipped: lock already acquired",
			slog.String("week_key", weekKey),
		)
		return nil
	}
	defer func() { _ = s.locker.Release(ctx, lockKey, token) }()

	rooms, err := s.service.ListSubscribedRooms(ctx)
	if err != nil {
		return fmt.Errorf("list subscribed rooms: %w", err)
	}
	if len(rooms) == 0 {
		s.logger.Info("Member news weekly skipped: no subscribed room")
		return nil
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		result delivery.SendResult
		sem    = make(chan struct{}, maxConcurrentDigests)
	)

	for i := range rooms {
		sem <- struct{}{} // 세마포어 획득
		wg.Add(1)
		go func(roomID string) {
			defer wg.Done()
			defer func() { <-sem }()

			roomResult := s.processRoomDigest(ctx, weekKey, roomID)
			mu.Lock()
			result.Merge(roomResult)
			mu.Unlock()
		}(rooms[i].RoomID)
	}
	wg.Wait()

	s.logger.Info("Member news weekly result",
		slog.String("week_key", weekKey),
		slog.Int("attempted", result.Attempted),
		slog.Int("sent", result.Sent),
		slog.Int("skipped", result.Skipped),
		slog.Int("failed", result.Failed),
		slog.Any("failed_rooms", result.FailedRooms),
	)

	if result.Sent == 0 && result.Failed > 0 {
		return fmt.Errorf("all %d room(s) failed to receive member news", result.Failed)
	}

	return nil
}

// processRoomDigest: 단일 room의 주간 다이제스트 생성 + outbox enqueue.
func (s *Scheduler) processRoomDigest(ctx context.Context, weekKey, roomID string) delivery.SendResult {
	return processDigestForRoom(ctx, s.service, s.formatter, s.outboxRepo, s.logger,
		PeriodWeekly, domain.DeliveryKindMemberNewsWeekly, weekKey, roomID, "🗞️ 이번주 구독 멤버 뉴스")
}

func startOfWeek(t time.Time) time.Time {
	kstNow := t.In(kst)
	daysFromMonday := (int(kstNow.Weekday()) - int(time.Monday) + 7) % 7
	start := kstNow.AddDate(0, 0, -daysFromMonday)
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, kst)
}
