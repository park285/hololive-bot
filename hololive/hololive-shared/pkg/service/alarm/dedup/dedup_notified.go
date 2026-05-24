package dedup

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func (s *Service) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	key := keys.NotifiedKey(streamID)
	scheduledStr := keys.FormatScheduled(startScheduled)

	existing, source, err := s.loadNotifiedData(ctx, key)
	if err != nil {
		return fmt.Errorf("mark as notified: load existing data: %w", err)
	}

	existing, source, err = s.resetStaleNotifiedData(ctx, key, existing, source, scheduledStr)
	if err != nil {
		return err
	}

	if source == notifiedDataSourceLegacyString {
		return s.markLegacyNotified(ctx, key, existing, scheduledStr, minutesUntil)
	}

	return s.writeNotifiedHashFields(ctx, key, scheduledStr, minutesUntil)
}

func (s *Service) resetStaleNotifiedData(
	ctx context.Context,
	key string,
	existing *NotifiedData,
	source notifiedDataSource,
	scheduledStr string,
) (*NotifiedData, notifiedDataSource, error) {
	if existing == nil || existing.StartScheduled == "" || existing.StartScheduled == scheduledStr {
		return existing, source, nil
	}

	if err := s.cache.Del(ctx, key); err != nil {
		return nil, source, fmt.Errorf("mark as notified: reset old schedule hash: %w", err)
	}
	return nil, notifiedDataSourceMissing, nil
}

func (s *Service) markLegacyNotified(ctx context.Context, key string, existing *NotifiedData, scheduledStr string, minutesUntil int) error {
	if existing == nil {
		existing = &NotifiedData{}
	}
	if existing.SentAt == nil {
		existing.SentAt = make(map[int]bool)
	}
	existing.StartScheduled = scheduledStr
	existing.SentAt[minutesUntil] = true
	if err := s.migrateLegacyNotifiedData(ctx, key, existing); err != nil {
		return fmt.Errorf("mark as notified: migrate legacy data: %w", err)
	}
	return nil
}

func (s *Service) writeNotifiedHashFields(ctx context.Context, key string, scheduledStr string, minutesUntil int) error {
	fields := map[string]any{
		"start_scheduled":          scheduledStr,
		strconv.Itoa(minutesUntil): "1",
	}
	if err := s.cache.HMSet(ctx, key, fields); err != nil {
		return fmt.Errorf("mark as notified: hmset fields: %w", err)
	}
	if err := s.cache.Expire(ctx, key, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("mark as notified: set expiration: %w", err)
	}
	return nil
}

func (s *Service) IsAlreadyNotifiedForSchedule(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) (bool, error) {
	key := keys.NotifiedKey(streamID)
	scheduledStr := keys.FormatScheduled(startScheduled)

	data, err := s.readNotifiedData(ctx, key)
	if err != nil {
		return false, fmt.Errorf("is already notified for schedule: %w", err)
	}
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

	targetPolicy := s.targetPolicySnapshot()
	targetMinutes := targetPolicy.Clone()

	if targetPolicy.Contains(minutesUntil) {
		return targetMinuteAlreadySent(data.SentAt, targetMinutes), nil
	}

	// non-target: 해당 분만 확인
	return data.SentAt[minutesUntil], nil
}

func targetMinuteAlreadySent(sentAt map[int]bool, targetMinutes []int) bool {
	for _, target := range targetMinutes {
		if sentAt[target] {
			return true
		}
	}
	return false
}

func (s *Service) IsAlreadyNotified(ctx context.Context, streamID string) (bool, error) {
	key := keys.NotifiedKey(streamID)
	data, err := s.readNotifiedData(ctx, key)
	if err != nil {
		return false, fmt.Errorf("is already notified: %w", err)
	}
	if data == nil {
		return false, nil
	}
	return len(data.SentAt) > 0, nil
}

func (s *Service) RecentlyNotifiedStreamIDs(ctx context.Context, streamIDs []string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if s == nil || len(streamIDs) == 0 {
		return result, nil
	}

	for _, streamID := range uniqueNonEmptyStrings(streamIDs) {
		data, err := s.readNotifiedData(ctx, keys.NotifiedKey(streamID))
		if err != nil {
			return nil, fmt.Errorf("recently notified stream ids: read %s: %w", streamID, err)
		}
		if data == nil || len(data.SentAt) == 0 {
			continue
		}
		result[streamID] = struct{}{}
	}

	return result, nil
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
