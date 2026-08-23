package alarmcache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	dedup "github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/park285/shared-go/v2/pkg/stringutil"
)

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
	titleFingerprint := keys.BuildTitleFingerprint(title, streamID)

	return fmt.Sprintf(
		"%s%s:%s:%d:%s", keys.UpcomingEventKeyPrefix, roomID,
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

	data := dedup.UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.Cache.Set(ctx, key, data, constants.CacheTTL.NotificationSent); err != nil {
		s.Logger.Warn("Failed to mark upcoming event notified",
			privacylog.RoomIDAttr(roomID),
			slog.String("channel_id", resolvedChannelID),
			slog.String("stream_id", stream.ID),
			slog.Int64("scheduled_minute", keys.NormalizeScheduledMinute(*stream.StartScheduled).Unix()),
			slog.Any("error", err),
		)

		return fmt.Errorf("mark upcoming event notified: %w", err)
	}

	return nil
}
