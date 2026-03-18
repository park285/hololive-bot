package settlement

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// SendMessageFunc: 메시지 전송 함수 타입.
type SendMessageFunc func(ctx context.Context, room, message string) error

// FormatAlarmFunc: 미납 알람 포맷 함수 타입.
type FormatAlarmFunc func(cycle *Cycle, unpaidNames []string) string

// Scheduler: 정산 알람 스케줄러.
type Scheduler struct {
	svc         *Service
	cache       cache.Client
	formatAlarm FormatAlarmFunc
	sendMessage SendMessageFunc
	roomID      string
	logger      *slog.Logger
}

// NewScheduler: 정산 스케줄러 인스턴스를 생성합니다.
func NewScheduler(
	svc *Service,
	cache cache.Client,
	formatAlarm FormatAlarmFunc,
	sendMessage SendMessageFunc,
	roomID string,
	logger *slog.Logger,
) *Scheduler {
	return &Scheduler{
		svc:         svc,
		cache:       cache,
		formatAlarm: formatAlarm,
		sendMessage: sendMessage,
		roomID:      roomID,
		logger:      logger,
	}
}

// Start: 매시간 정산 알람 체크 루프를 시작합니다.
func (s *Scheduler) Start(ctx context.Context) {
	if s.roomID == "" {
		s.logger.Info("Settlement scheduler disabled: no room ID configured")
		return
	}

	s.logger.Info("Settlement scheduler started", slog.String("room", s.roomID))

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// 시작 시 즉시 1회 체크
	s.check(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Settlement scheduler stopped")
			return
		case <-ticker.C:
			s.check(ctx)
		}
	}
}

func (s *Scheduler) check(ctx context.Context) {
	now := time.Now().UTC()

	cycle, unpaid, err := s.svc.GetReminderPayload(ctx, s.roomID, now)
	if err != nil {
		s.logger.Error("정산 알람 대상 조회 실패", slog.String("error", err.Error()))
		return
	}
	if cycle == nil || len(unpaid) == 0 {
		return
	}

	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		s.logger.Error("KST 로케이션 로드 실패", slog.String("error", err.Error()))
		return
	}
	localNow := now.In(kst)

	dedupKey := fmt.Sprintf(
		"settlement:alarm:%s:%s:%s",
		s.roomID,
		cycle.CycleKey,
		localNow.Format("2006-01-02"),
	)

	exists, err := s.cache.Exists(ctx, dedupKey)
	if err != nil {
		s.logger.Error("dedup 키 확인 실패", slog.String("error", err.Error()))
		return
	}
	if exists {
		return
	}

	msg := s.formatAlarm(cycle, unpaid)
	if err := s.sendMessage(ctx, s.roomID, msg); err != nil {
		s.logger.Error("정산 알람 발송 실패", slog.String("error", err.Error()))
		return
	}

	if err := s.cache.Set(ctx, dedupKey, true, 24*time.Hour); err != nil {
		s.logger.Error("dedup 키 설정 실패", slog.String("error", err.Error()))
	}

	s.logger.Info("정산 알람 발송 완료",
		slog.String("cycle_key", cycle.CycleKey),
		slog.Int("unpaid_count", len(unpaid)),
	)
}
