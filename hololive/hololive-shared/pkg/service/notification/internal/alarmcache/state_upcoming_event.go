package alarmcache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/park285/shared-go/pkg/stringutil"
)

func BuildTitleFingerprint(title, streamID string) string {
	return keys.BuildTitleFingerprint(title, streamID)
}

func ResolveStreamChannelID(stream *domain.Stream, defaultChannelID string) string {
	if stream == nil {
		return defaultChannelID
	}

	channelID := stringutil.TrimSpace(stream.ChannelID)
	if channelID != "" {
		return channelID
	}

	if stream.Channel != nil {
		channelID = stringutil.TrimSpace(stream.Channel.ID)
		if channelID != "" {
			return channelID
		}
	}

	return defaultChannelID
}

func (s *State) BuildUpcomingEventKey(roomID, channelID, streamID, title string, startScheduled time.Time) string {
	scheduledMinute := NormalizeScheduledMinute(startScheduled).Unix()
	titleFingerprint := BuildTitleFingerprint(title, streamID)

	return fmt.Sprintf(
		"%s%s:%s:%d:%s",
		UpcomingEventKeyPrefix,
		roomID,
		channelID,
		scheduledMinute,
		titleFingerprint,
	)
}

func (s *State) MarkUpcomingEventNotified(
	ctx context.Context,
	roomID, channelID string,
	stream *domain.Stream,
) error {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil
	}

	resolvedChannelID := ResolveStreamChannelID(stream, channelID)
	if stringutil.TrimSpace(resolvedChannelID) == "" {
		return nil
	}

	key := s.BuildUpcomingEventKey(roomID, resolvedChannelID, stream.ID, stream.Title, *stream.StartScheduled)

	data := UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.Cache.Set(ctx, key, data, constants.CacheTTL.NotificationSent); err != nil {
		s.Logger.Warn("Failed to mark upcoming event notified",
			slog.String("key", key),
			slog.String("room_id", roomID),
			slog.String("channel_id", resolvedChannelID),
			slog.String("stream_id", stream.ID),
			slog.Any("error", err),
		)

		return fmt.Errorf("mark upcoming event notified: %w", err)
	}

	return nil
}

func (s *State) WasUpcomingEventNotifiedRecently(
	ctx context.Context,
	roomID, channelID string,
	stream *domain.Stream,
	window time.Duration,
) bool {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return false
	}

	resolvedChannelID := ResolveStreamChannelID(stream, channelID)
	if stringutil.TrimSpace(resolvedChannelID) == "" {
		return false
	}

	key := s.BuildUpcomingEventKey(roomID, resolvedChannelID, stream.ID, stream.Title, *stream.StartScheduled)

	var data UpcomingEventNotifiedData
	if err := s.Cache.Get(ctx, key, &data); err != nil || data.NotifiedAt == "" {
		return false
	}

	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	if err != nil {
		return false
	}

	if window <= 0 {
		return false
	}

	return time.Since(notifiedAt) <= window
}
