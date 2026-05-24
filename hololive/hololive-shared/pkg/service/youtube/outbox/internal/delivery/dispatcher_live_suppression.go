package delivery

import (
	"context"
	"log/slog"
	"time"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type liveUpcomingSuppressionData struct {
	NotifiedAt string `json:"notified_at"`
}

func (d *Dispatcher) filterLiveCatchupSuppressedRooms(
	ctx context.Context,
	item domain.YouTubeNotificationOutbox,
	rooms map[string]bool,
) map[string]bool {
	if !shouldFilterLiveCatchupSuppression(d, item, rooms) {
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
		suppressed := d.wasLiveCatchupRecentlyCoveredByUpcoming(ctx, roomID, item.ChannelID, payload)
		if !suppressed {
			filtered[roomID] = true
		}
	}
	return filtered
}

func shouldFilterLiveCatchupSuppression(d *Dispatcher, item domain.YouTubeNotificationOutbox, rooms map[string]bool) bool {
	return d != nil &&
		d.cache != nil &&
		item.Kind == domain.OutboxKindLiveStream &&
		len(rooms) > 0
}

func liveStreamPayloadForSuppression(item domain.YouTubeNotificationOutbox) (videoPayload, bool) {
	var payload videoPayload
	if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
		return videoPayload{}, false
	}
	scheduledAt := liveSuppressionScheduledAt(payload)
	return payload, payload.VideoID != "" && payload.Title != "" && scheduledAt != nil && !scheduledAt.IsZero()
}

func (d *Dispatcher) wasLiveCatchupRecentlyCoveredByUpcoming(
	ctx context.Context,
	roomID string,
	channelID string,
	payload videoPayload,
) bool {
	scheduledAt := liveSuppressionScheduledAt(payload)
	if scheduledAt == nil || scheduledAt.IsZero() {
		return false
	}
	key := keys.BuildUpcomingEventKey(roomID, channelID, payload.VideoID, payload.Title, scheduledAt.UTC())
	var data liveUpcomingSuppressionData
	if err := d.cache.Get(ctx, key, &data); err != nil {
		d.logger.Warn("Failed to read live catchup upcoming suppression marker",
			slog.String("room_id", roomID),
			slog.String("channel_id", channelID),
			slog.String("video_id", payload.VideoID),
			slog.Any("error", err))
		return false
	}
	if data.NotifiedAt == "" {
		return false
	}
	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	if err != nil {
		return false
	}
	return time.Since(notifiedAt) <= constants.LiveCatchupSuppressWindow
}

func liveSuppressionScheduledAt(payload videoPayload) *time.Time {
	if payload.ScheduledStartAt != nil && !payload.ScheduledStartAt.IsZero() {
		return payload.ScheduledStartAt
	}
	return payload.PublishedAt
}
