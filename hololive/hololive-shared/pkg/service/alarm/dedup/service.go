package dedup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// NotifiedData: 알림 중복 발송 방지를 위한 이력 정보
type NotifiedData struct {
	StartScheduled string       `json:"start_scheduled"`
	SentAt         map[int]bool `json:"sent_at"`
}

// UpcomingEventNotifiedData: 예정 알림 발송 시각 기록
type UpcomingEventNotifiedData struct {
	NotifiedAt string `json:"notified_at"`
}

// Service: SETNX 기반 4단계 알림 중복 방지 서비스
type Service struct {
	cache         cache.Client
	targetMinutes []int
	fallback      *LocalFallback
	logger        *slog.Logger
}

// NewService: DedupService 생성
func NewService(c cache.Client, targetMinutes []int, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		cache:         c,
		targetMinutes: targetMinutes,
		fallback:      NewLocalFallback(logger),
		logger:        logger,
	}
}

// TryClaimNotification: 알림 발송 권한 선점 시도
// startScheduled가 zero이면 ("", false, nil) 반환
func (s *Service) TryClaimNotification(ctx context.Context, roomID, streamID string, startScheduled time.Time, minutesUntil int) (string, bool, error) {
	if startScheduled.IsZero() {
		return "", false, nil
	}

	category := keys.NotificationCategory(s.targetMinutes, minutesUntil)
	key := keys.BuildNotifyClaimKey(roomID, streamID, startScheduled, category)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

// TryClaimLogicalEvent: 논리적 이벤트 claim 시도 (stream_id 변경 대응)
func (s *Service) TryClaimLogicalEvent(ctx context.Context, roomID, channelID string, stream *domain.Stream, minutesUntil int) (string, bool, error) {
	if stream == nil {
		return "", false, nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return "", false, nil
	}

	category := keys.NotificationCategory(s.targetMinutes, minutesUntil)
	key := keys.BuildLogicalEventClaimKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled, category)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

// TryClaimScheduleTransition: 일정 변경 전환 claim 시도
func (s *Service) TryClaimScheduleTransition(ctx context.Context, streamID string, oldScheduled, newScheduled time.Time) (string, bool, error) {
	key := keys.BuildScheduleTransitionKey(streamID, oldScheduled, newScheduled)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

// ReleaseClaims: claim 키 해제 (발송 실패 시 재시도 허용)
func (s *Service) ReleaseClaims(ctx context.Context, claimKeys []string) error {
	if len(claimKeys) == 0 {
		return nil
	}
	// 로컬 폴백 해제
	s.fallback.ReleaseClaims(claimKeys)

	// Valkey DEL
	_, err := s.cache.DelMany(ctx, claimKeys)
	if err != nil {
		return fmt.Errorf("release claims: del many keys: %w", err)
	}
	return nil
}

// MarkAsNotified: 알림 발송 이력 기록 (HSET 원자적 갱신)
func (s *Service) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	key := keys.NotifiedKey(streamID)
	scheduledStr := keys.FormatScheduled(startScheduled)

	// 기존 스케줄 확인 (변경 감지)
	existingScheduled, err := s.cache.HGet(ctx, key, "start_scheduled")
	if err != nil {
		return fmt.Errorf("mark as notified: get existing schedule: %w", err)
	}
	if existingScheduled != "" && existingScheduled != scheduledStr {
		// 스케줄 변경 -> 기존 해시 삭제 후 재생성
		if err := s.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("mark as notified: reset old schedule hash: %w", err)
		}
	}

	// 원자적 필드 갱신
	if err := s.cache.HSet(ctx, key, "start_scheduled", scheduledStr); err != nil {
		return fmt.Errorf("mark as notified: set start_scheduled field: %w", err)
	}
	if err := s.cache.HSet(ctx, key, strconv.Itoa(minutesUntil), "1"); err != nil {
		return fmt.Errorf("mark as notified: set minute field: %w", err)
	}
	if err := s.cache.Expire(ctx, key, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("mark as notified: set expiration: %w", err)
	}
	return nil
}

// IsAlreadyNotifiedForSchedule: 현재 스케줄에서 해당 분에 이미 알림 발송됐는지 확인
func (s *Service) IsAlreadyNotifiedForSchedule(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) (bool, error) {
	key := keys.NotifiedKey(streamID)
	scheduledStr := keys.FormatScheduled(startScheduled)

	data := s.readNotifiedData(ctx, key)
	if data == nil {
		return false, nil
	}

	// 스케줄 변경됨 -> 발송 허용
	if data.StartScheduled != scheduledStr {
		return false, nil
	}

	// live catchup: SentAt[0] 확인
	if minutesUntil == 0 {
		return data.SentAt[0], nil
	}

	// target 분: 어떤 target이라도 발송됐으면 차단 (1회 정책)
	if s.isTargetMinute(minutesUntil) {
		for _, target := range s.targetMinutes {
			if data.SentAt[target] {
				return true, nil
			}
		}
		return false, nil
	}

	// non-target: 해당 분만 확인
	return data.SentAt[minutesUntil], nil
}

// IsAlreadyNotified: 어떤 분이라도 발송 이력이 있으면 true
func (s *Service) IsAlreadyNotified(ctx context.Context, streamID string) (bool, error) {
	key := keys.NotifiedKey(streamID)
	data := s.readNotifiedData(ctx, key)
	if data == nil {
		return false, nil
	}
	return len(data.SentAt) > 0, nil
}

// MarkUpcomingEventNotified: 예정 알림 발송 시각을 이벤트 단위로 기록
func (s *Service) MarkUpcomingEventNotified(ctx context.Context, roomID, channelID string, stream *domain.Stream) error {
	if stream == nil {
		return nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil
	}

	key := keys.BuildUpcomingEventKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled)
	data := UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.cache.Set(ctx, key, data, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("mark upcoming event notified: set cache key: %w", err)
	}
	return nil
}

// WasUpcomingEventNotifiedRecently: 동일 이벤트의 예정 알림이 window 내에 발송됐는지 확인
func (s *Service) WasUpcomingEventNotifiedRecently(ctx context.Context, roomID, channelID string, stream *domain.Stream, window time.Duration) (bool, error) {
	if stream == nil {
		return false, nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return false, nil
	}
	if window <= 0 {
		return false, nil
	}

	key := keys.BuildUpcomingEventKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled)

	var data UpcomingEventNotifiedData
	if err := s.cache.Get(ctx, key, &data); err != nil {
		var raw string
		if rawErr := s.cache.Get(ctx, key, &raw); rawErr != nil {
			return false, fmt.Errorf("was upcoming event notified recently: unmarshal data: %w", rawErr)
		}
		if raw == "" {
			return false, nil
		}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return false, fmt.Errorf("was upcoming event notified recently: unmarshal data: %w", err)
		}
	}
	if data.NotifiedAt == "" {
		return false, nil
	}

	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	if err != nil {
		return false, nil
	}

	return time.Since(notifiedAt) <= window, nil
}

// --- 비공개 헬퍼 ---

// tryClaimKey: SETNX 기반 키 선점 (Valkey 장애 시 로컬 폴백)
func (s *Service) tryClaimKey(ctx context.Context, key string, ttl time.Duration) bool {
	acquired, err := s.cache.SetNX(ctx, key, "1", ttl)
	if err != nil {
		return s.fallback.TryClaimOnOutage(key, ttl, err)
	}
	return acquired
}

func (s *Service) isTargetMinute(minutesUntil int) bool {
	return slices.Contains(s.targetMinutes, minutesUntil)
}

// readNotifiedData: Valkey에서 NotifiedData 조회 (HGETALL 우선, 기존 JSON 폴백)
func (s *Service) readNotifiedData(ctx context.Context, key string) *NotifiedData {
	// HGETALL로 해시 데이터 조회 시도
	fields, err := s.cache.HGetAll(ctx, key)
	if err == nil && len(fields) > 0 {
		startScheduled := fields["start_scheduled"]
		sentAt := make(map[int]bool)
		for k := range fields {
			if k == "start_scheduled" {
				continue
			}
			if m, err := strconv.Atoi(k); err == nil {
				sentAt[m] = true
			}
		}
		return &NotifiedData{
			StartScheduled: startScheduled,
			SentAt:         sentAt,
		}
	}

	// 폴백: 기존 JSON 형식 (GET -> parse)
	var data NotifiedData
	if err := s.cache.Get(ctx, key, &data); err == nil {
		if data.StartScheduled != "" || len(data.SentAt) > 0 {
			return &data
		}
	}

	// 2차 폴백: JSON 문자열로 감싼 legacy 포맷
	var raw string
	if err := s.cache.Get(ctx, key, &raw); err != nil || raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}
	return &data
}
