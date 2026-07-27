package alarmcache

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/park285/shared-go/pkg/logging"
)

func NormalizeScheduledMinute(startScheduled time.Time) time.Time {
	return startScheduled.Truncate(time.Minute)
}

func NotifiedMinuteKey(streamID string, startScheduled time.Time, minutesUntil int) string {
	normalizedScheduled := NormalizeScheduledMinute(startScheduled).Unix()

	return fmt.Sprintf(
		"%s%s:%d:%d", sharedalarmkeys.NotifiedKeyPrefix, strings.TrimSpace(streamID),
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

	return false
}
