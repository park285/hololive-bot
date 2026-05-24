package alarmcache

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/park285/shared-go/pkg/logging"
)

func NormalizeScheduledMinute(startScheduled time.Time) time.Time {
	return startScheduled.Truncate(time.Minute)
}

func NotifiedMinuteKey(streamID string, startScheduled time.Time, minutesUntil int) string {
	normalizedScheduled := NormalizeScheduledMinute(startScheduled).Unix()

	return fmt.Sprintf(
		"%s%s:%d:%d",
		NotifiedKeyPrefix,
		strings.TrimSpace(streamID),
		normalizedScheduled,
		minutesUntil,
	)
}

func (s *State) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return fmt.Errorf("mark as notified: stream id is empty")
	}

	canonicalKey := NotifiedMinuteKey(streamID, startScheduled, minutesUntil)
	if err := s.Cache.Set(ctx, canonicalKey, "1", constants.CacheTTL.NotificationSent); err != nil {
		return logging.LogAndWrapError(ctx, s.Logger, "mark as notified", err,
			slog.String("stream_id", streamID),
			slog.Int("minutes_until", minutesUntil),
		)
	}

	if err := s.UpdateLegacyNotifiedData(ctx, streamID, startScheduled, minutesUntil); err != nil {
		return logging.LogAndWrapError(ctx, s.Logger, "mark as notified legacy data", err,
			slog.String("stream_id", streamID),
			slog.Int("minutes_until", minutesUntil),
		)
	}

	return nil
}

func (s *State) UpdateLegacyNotifiedData(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	s.NotifiedLegacyMu.Lock()
	defer s.NotifiedLegacyMu.Unlock()

	notifiedKey := NotifiedKeyPrefix + streamID
	scheduledStr := NormalizeScheduledMinute(startScheduled).Format(time.RFC3339)

	var existing NotifiedData
	if err := s.Cache.Get(ctx, notifiedKey, &existing); err != nil || existing.StartScheduled == "" {
		existing = NotifiedData{StartScheduled: scheduledStr, SentAt: make(map[int]bool)}
	}

	if existing.StartScheduled != scheduledStr {
		existing = NotifiedData{StartScheduled: scheduledStr, SentAt: make(map[int]bool)}
	}
	if existing.SentAt == nil {
		existing.SentAt = make(map[int]bool)
	}

	existing.SentAt[minutesUntil] = true

	if err := s.Cache.Set(ctx, notifiedKey, existing, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("set legacy notified data: %w", err)
	}

	return nil
}

func (s *State) WasNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) bool {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return false
	}

	var marker string
	if err := s.Cache.Get(ctx, NotifiedMinuteKey(streamID, startScheduled, minutesUntil), &marker); err == nil && marker == "1" {
		return true
	}

	var legacy NotifiedData
	if err := s.Cache.Get(ctx, NotifiedKeyPrefix+streamID, &legacy); err != nil {
		return false
	}
	if legacy.StartScheduled != NormalizeScheduledMinute(startScheduled).Format(time.RFC3339) {
		return false
	}

	return legacy.SentAt != nil && legacy.SentAt[minutesUntil]
}
