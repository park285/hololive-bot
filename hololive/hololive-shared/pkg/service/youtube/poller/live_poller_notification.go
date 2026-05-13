package poller

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func streamsFromUpcomingEvents(channelID string, events []*scraper.UpcomingEvent) []*domain.Stream {
	streams := make([]*domain.Stream, 0, len(events))
	for _, event := range events {
		stream := streamFromUpcomingEvent(channelID, event)
		if stream == nil {
			continue
		}
		streams = append(streams, stream)
	}
	return streams
}

func streamFromUpcomingEvent(channelID string, event *scraper.UpcomingEvent) *domain.Stream {
	if event == nil {
		return nil
	}
	stream := &domain.Stream{
		ID:          event.VideoID,
		ChannelID:   channelID,
		ChannelName: event.ChannelTitle,
		Title:       event.Title,
		Status:      streamStatusFromEvent(event.Status),
	}
	if event.StartTime != nil {
		startTime := time.Unix(*event.StartTime, 0)
		stream.StartScheduled = &startTime
	}
	if len(event.Thumbnail) > 0 {
		thumbnail := event.Thumbnail[len(event.Thumbnail)-1].URL
		stream.Thumbnail = &thumbnail
	}
	link := fmt.Sprintf("https://www.youtube.com/watch?v=%s", event.VideoID)
	stream.Link = &link
	if viewers := parseViewerCount(event.ViewCountText); viewers > 0 {
		stream.ViewerCount = &viewers
	}
	return stream
}

func streamStatusFromEvent(eventStatus string) domain.StreamStatus {
	switch eventStatus {
	case "LIVE":
		return domain.StreamStatusLive
	case "UPCOMING":
		return domain.StreamStatusUpcoming
	default:
		return ""
	}
}

func insertLiveNotification(ctx context.Context, tx *gorm.DB, channelID string, stream *domain.Stream, now time.Time) error {
	notification := &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindLiveStream,
		ChannelID:     firstNonEmpty(stream.ChannelID, channelID),
		ContentID:     stream.ID,
		Payload:       buildLiveNotificationPayload(channelID, stream),
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: now.UTC(),
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "kind"}, {Name: "content_id"}},
		DoNothing: true,
	}).Create(notification).Error
}

func buildLiveNotificationPayload(channelID string, stream *domain.Stream) string {
	return mustMarshalJSON(&domain.YouTubeVideo{
		VideoID:     stream.ID,
		ChannelID:   firstNonEmpty(stream.ChannelID, channelID),
		Title:       stream.Title,
		Thumbnail:   thumbnailFromStream(stream),
		PublishedAt: livePayloadPublishedAt(stream),
		IsShort:     false,
	})
}

func thumbnailFromStream(stream *domain.Stream) domain.ThumbnailsJSON {
	if stream == nil || stream.Thumbnail == nil || *stream.Thumbnail == "" {
		return nil
	}
	return domain.ThumbnailsJSON{{URL: *stream.Thumbnail}}
}

func livePayloadPublishedAt(stream *domain.Stream) *time.Time {
	if stream == nil {
		return nil
	}
	if stream.StartActual != nil && !stream.StartActual.IsZero() {
		startedAt := stream.StartActual.UTC()
		return &startedAt
	}
	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		scheduledAt := stream.StartScheduled.UTC()
		return &scheduledAt
	}
	return nil
}
