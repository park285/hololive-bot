package youtubedispatch

import (
	"context"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
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

	filtered := make(map[string]bool, len(rooms))
	for roomID, selected := range rooms {
		if !selected {
			continue
		}

		suppressed := g.wasLiveCatchupRecentlyCoveredByUpcoming(ctx, roomID, item.ChannelID, &payload)
		if !suppressed {
			filtered[roomID] = true
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
	ctx context.Context,
	roomID string,
	channelID string,
	payload *youtubeoutbox.Video,
) bool {
	scheduledAt := liveSuppressionScheduledAt(payload)
	if scheduledAt == nil || scheduledAt.IsZero() {
		return false
	}

	key := keys.BuildUpcomingEventKey(roomID, channelID, payload.VideoID, payload.Title, scheduledAt.UTC())

	var data liveUpcomingSuppressionData

	if err := g.cache.Get(ctx, key, &data); err != nil {
		g.logger.Warn("Failed to read live catchup upcoming suppression marker",
			slog.String("room_id", roomID),
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
			slog.String("room_id", roomID),
			slog.String("channel_id", channelID),
			slog.String("video_id", payload.VideoID),
			slog.String("notified_at", data.NotifiedAt),
			slog.Any("error", err))
		observeLiveCatchupSuppression(liveCatchupSuppressionResultInvalidMarker)

		return false
	}

	if time.Since(notifiedAt) > constants.LiveCatchupSuppressWindow {
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
