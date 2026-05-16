package polling

import (
	"fmt"
	"time"

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
