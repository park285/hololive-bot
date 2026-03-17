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
type FormatAlarmFunc func(unpaidNames []string, perPerson int, dueDay int) string

// Scheduler: 정산 알람 스케줄러.
type Scheduler struct {
	repo        *Repository
	cache       cache.Client
	formatAlarm FormatAlarmFunc
	sendMessage SendMessageFunc
	roomID      string
	logger      *slog.Logger
}

// NewScheduler: 정산 스케줄러 인스턴스를 생성합니다.
func NewScheduler(
	repo *Repository,
	cache cache.Client,
	formatAlarm FormatAlarmFunc,
	sendMessage SendMessageFunc,
	roomID string,
	logger *slog.Logger,
) *Scheduler {
	return &Scheduler{
		repo:        repo,
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
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		s.logger.Error("KST 로케이션 로드 실패", slog.String("error", err.Error()))
		return
	}

	now := time.Now().In(kst)
	// 17일부터 말일까지 매일 알람 발송
	if now.Day() < 17 {
		return
	}

	year, month, day := now.Year(), int(now.Month()), now.Day()
	dedupKey := fmt.Sprintf("settlement:alarm:%s:%d:%d:%d", s.roomID, year, month, day)

	// Valkey dedup: 이미 발송했으면 스킵
	exists, err := s.cache.Exists(ctx, dedupKey)
	if err != nil {
		s.logger.Error("dedup 키 확인 실패", slog.String("error", err.Error()))
		return
	}
	if exists {
		return
	}

	// 사이클 확보 + 납부 행 생성
	cycle, err := s.repo.EnsureCycle(ctx, s.roomID, year, month)
	if err != nil {
		s.logger.Error("사이클 확보 실패", slog.String("error", err.Error()))
		return
	}

	if err := s.repo.EnsurePaymentRows(ctx, s.roomID, cycle.ID); err != nil {
		s.logger.Error("납부 행 생성 실패", slog.String("error", err.Error()))
		return
	}

	unpaid, err := s.repo.GetUnpaidMembers(ctx, cycle.ID)
	if err != nil {
		s.logger.Error("미납 멤버 조회 실패", slog.String("error", err.Error()))
		return
	}

	if len(unpaid) == 0 {
		return
	}

	// 알람 메시지 발송
	msg := s.formatAlarm(unpaid, cycle.PerPerson, cycle.DueDay)
	if err := s.sendMessage(ctx, s.roomID, msg); err != nil {
		s.logger.Error("정산 알람 발송 실패", slog.String("error", err.Error()))
		return
	}

	// dedup 키 설정 (24시간 TTL)
	if err := s.cache.Set(ctx, dedupKey, true, 24*time.Hour); err != nil {
		s.logger.Error("dedup 키 설정 실패", slog.String("error", err.Error()))
	}

	s.logger.Info("정산 알람 발송 완료",
		slog.Int("year", year),
		slog.Int("month", month),
		slog.Int("unpaid_count", len(unpaid)),
	)
}
