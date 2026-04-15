package holodex

import (
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func (s *ScraperService) convertEventToStream(event *scraper.UpcomingEvent, channelID string) *domain.Stream {
	stream := &domain.Stream{
		ID:          event.VideoID,
		Title:       event.Title,
		ChannelID:   channelID,
		ChannelName: event.ChannelTitle,
		Status:      s.mapEventStatus(event.Status),
	}

	if len(event.Thumbnail) > 0 {
		bestThumb := event.Thumbnail[len(event.Thumbnail)-1].URL
		stream.Thumbnail = &bestThumb
	} else {
		thumbURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", event.VideoID)
		stream.Thumbnail = &thumbURL
	}

	linkURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", event.VideoID)
	stream.Link = &linkURL

	if event.StartTime != nil {
		startTime := time.Unix(*event.StartTime, 0)
		stream.StartScheduled = &startTime
	}

	return stream
}

func (s *ScraperService) mapEventStatus(status string) domain.StreamStatus {
	switch status {
	case "LIVE":
		return domain.StreamStatusLive
	case "UPCOMING":
		return domain.StreamStatusUpcoming
	default:
		return domain.StreamStatusUpcoming
	}
}
