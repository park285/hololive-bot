package dedup

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/constants"
)

func (s *Service) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	key := keys.NotifiedKey(streamID)
	scheduledStr := keys.FormatScheduled(startScheduled)

	existing, _, err := s.loadNotifiedData(ctx, key)
	if err != nil {
		return fmt.Errorf("mark as notified: load existing data: %w", err)
	}

	if err := s.resetStaleNotifiedData(ctx, key, existing, scheduledStr); err != nil {
		return fmt.Errorf("reset stale notified data: %w", err)
	}

	if err := s.writeNotifiedHashFields(ctx, key, scheduledStr, minutesUntil); err != nil {
		return fmt.Errorf("write notified hash fields: %w", err)
	}

	return nil
}

func (s *Service) resetStaleNotifiedData(
	ctx context.Context,
	key string,
	existing NotifiedData,
	scheduledStr string,
) error {
	if existing.StartScheduled == "" || existing.StartScheduled == scheduledStr {
		return nil
	}

	if err := s.cache.Del(ctx, key); err != nil {
		return fmt.Errorf("mark as notified: reset old schedule hash: %w", err)
	}

	return nil
}

func (s *Service) writeNotifiedHashFields(ctx context.Context, key, scheduledStr string, minutesUntil int) error {
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

// RecentlyNotifiedStreamIDs는 notified hash에 발송한 분이 하나라도 기록된 stream을 HGETALL 묶음 한 번으로 찾는다.
// 한 stream이라도 조회에 실패하면(구형 string 형상의 WRONGTYPE 포함) 결과 없이 오류를 돌려준다.
func (s *Service) RecentlyNotifiedStreamIDs(ctx context.Context, streamIDs []string) (map[string]struct{}, error) {
	result := make(map[string]struct{})

	if s == nil || len(streamIDs) == 0 {
		return result, nil
	}

	uniqueStreamIDs := uniqueNonEmptyStrings(streamIDs)
	if len(uniqueStreamIDs) == 0 {
		return result, nil
	}

	cmds := make([]valkey.Completed, 0, len(uniqueStreamIDs))
	for _, streamID := range uniqueStreamIDs {
		cmds = append(cmds, s.cache.B().Hgetall().Key(keys.NotifiedKey(streamID)).Build())
	}

	results := s.cache.DoMulti(ctx, cmds...)
	if len(results) != len(cmds) {
		return nil, fmt.Errorf("recently notified stream ids: unexpected result count %d for %d streams", len(results), len(cmds))
	}

	for i, streamID := range uniqueStreamIDs {
		fields, err := results[i].AsStrMap()
		if err != nil {
			return nil, fmt.Errorf("recently notified stream ids: read %s: %w", streamID, err)
		}

		if len(parseNotifiedHash(fields).SentAt) == 0 {
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
