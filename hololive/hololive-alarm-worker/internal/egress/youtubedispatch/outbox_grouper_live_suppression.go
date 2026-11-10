package youtubedispatch

import (
	"context"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

type liveUpcomingSuppressionData struct {
	NotifiedAt string `json:"notified_at"`
}

func (g *OutboxGrouper) filterLiveCatchupSuppressedRooms(
	ctx context.Context,
	item *domain.YouTubeNotificationOutbox,
	rooms map[string]bool,
) map[string]bool {
	if !shouldFilterLiveCatchupSuppression(g, item, rooms) {
		return rooms
	}

	payload, ok := liveStreamPayloadForSuppression(item)
	if !ok {
		return rooms
	}

	scheduledAt := liveSuppressionScheduledAt(&payload).UTC()
	roomIDs := make([]string, 0, len(rooms))
	commands := make([]valkey.Completed, 0, len(rooms))

	for roomID, selected := range rooms {
		if !selected {
			continue
		}

		roomIDs = append(roomIDs, roomID)
		commands = append(commands, g.cache.B().Get().Key(keys.BuildUpcomingEventKey(roomID, item.ChannelID, payload.VideoID, payload.Title, scheduledAt)).Build())
	}

	filtered := make(map[string]bool, len(roomIDs))
	if len(commands) == 0 {
		return filtered
	}

	for index, result := range g.cache.DoMulti(ctx, commands...) {
		if !g.wasLiveCatchupRecentlyCoveredByUpcoming(roomIDs[index], item.ChannelID, &payload, result) {
			filtered[roomIDs[index]] = true
		}
	}

	return filtered
}

func shouldFilterLiveCatchupSuppression(g *OutboxGrouper, item *domain.YouTubeNotificationOutbox, rooms map[string]bool) bool {
	return g != nil &&
		g.cache != nil &&
		item.Kind == domain.OutboxKindLiveStream &&
		len(rooms) > 0
}

func liveStreamPayloadForSuppression(item *domain.YouTubeNotificationOutbox) (youtubeoutbox.Video, bool) {
	var payload youtubeoutbox.Video

	if err := jsonv2.Unmarshal([]byte(item.Payload), &payload); err != nil {
		return youtubeoutbox.Video{}, false
	}

	scheduledAt := liveSuppressionScheduledAt(&payload)

	return payload, payload.VideoID != "" && payload.Title != "" && scheduledAt != nil && !scheduledAt.IsZero()
}

func (g *OutboxGrouper) wasLiveCatchupRecentlyCoveredByUpcoming(
	roomID string,
	channelID string,
	payload *youtubeoutbox.Video,
	result valkey.ValkeyResult,
) bool {
	raw, err := result.AsBytes()
	if valkey.IsValkeyNil(err) {
		return false
	}

	var data liveUpcomingSuppressionData

	if err == nil {
		err = jsonv2.Unmarshal(raw, &data)
	}

	if err != nil {
		g.logger.Warn("Failed to read live catchup upcoming suppression marker",
			privacylog.RoomIDAttr(roomID),
			slog.String("channel_id", channelID),
			slog.String("video_id", payload.VideoID),
			slog.Any("error", err))
		observeLiveCatchupSuppression(liveCatchupSuppressionResultCacheError)

		return false
	}

	if data.NotifiedAt == "" {
		return false
	}

	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	if err != nil {
		g.logger.Warn("Invalid live catchup upcoming suppression marker",
			privacylog.RoomIDAttr(roomID),
			slog.String("channel_id", channelID),
			slog.String("video_id", payload.VideoID),
			slog.String("notified_at", data.NotifiedAt),
			slog.Any("error", err))
		observeLiveCatchupSuppression(liveCatchupSuppressionResultInvalidMarker)

		return false
	}

	if time.Since(notifiedAt) > tier.LiveCatchupSuppressWindow {
		return false
	}

	observeLiveCatchupSuppression(liveCatchupSuppressionResultSuppressed)

	return true
}

func liveSuppressionScheduledAt(payload *youtubeoutbox.Video) *time.Time {
	if payload.ScheduledStartAt != nil && !payload.ScheduledStartAt.IsZero() {
		return payload.ScheduledStartAt
	}

	return payload.PublishedAt
}
