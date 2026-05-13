package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/fallback"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func upcomingCacheKey(channelIDs []string) string {
	sortedIDs := make([]string, len(channelIDs))
	copy(sortedIDs, channelIDs)
	slices.Sort(sortedIDs)
	return fmt.Sprintf("youtube:upcoming:%s", strings.Join(sortedIDs, ","))
}

func (ys *serviceImpl) scrapeUpcomingStreams(ctx context.Context, channelIDs []string) upcomingScrapeResult {
	result := upcomingScrapeResult{}
	var mu sync.Mutex

	primary := fallback.RunPrimary(ctx, channelIDs, fallback.FetchPlan[string, struct{}]{Parallelism: 5}, func(gctx context.Context, channelID string) error {
		events, err := ys.scraper.GetUpcomingEvents(gctx, channelID)
		if err != nil {
			return fmt.Errorf("scraper upcoming events for %s: %w", channelID, err)
		}
		if len(events) == 0 {
			return nil
		}

		streams := ys.convertScrapedEvents(events, channelID)
		mu.Lock()
		result.streams = append(result.streams, streams...)
		mu.Unlock()
		return nil
	})
	fallback.ObservePrimaryPhase("youtube", "upcoming_streams", len(channelIDs), primary.Succeeded, len(primary.Failed))

	result.failedIDs = primary.Failed
	result.scraped = primary.Succeeded
	ys.logger.Info("Scraper phase completed (upcoming streams)",
		slog.Int("total", len(channelIDs)),
		slog.Int("scraped", result.scraped),
		slog.Int("failed", len(result.failedIDs)))
	return result
}

func (ys *serviceImpl) convertScrapedEvents(events []*scraper.UpcomingEvent, channelID string) []*domain.Stream {
	if len(events) == 0 {
		return nil
	}

	streams := make([]*domain.Stream, 0, len(events))
	channelName := ys.scrapedEventChannelName(channelID, events[0])

	for _, event := range events {
		if !isScrapedEventStreamStatus(event.Status) {
			continue
		}

		streams = append(streams, ys.streamFromScrapedEvent(event, channelID, channelName))
	}

	return streams
}

func (ys *serviceImpl) scrapedEventChannelName(channelID string, event *scraper.UpcomingEvent) string {
	channelName := ys.getChannelName(channelID)
	if channelName != "" {
		return channelName
	}
	return event.ChannelTitle
}

func isScrapedEventStreamStatus(status string) bool {
	return status == "LIVE" || status == "UPCOMING"
}

func (ys *serviceImpl) streamFromScrapedEvent(event *scraper.UpcomingEvent, channelID string, channelName string) *domain.Stream {
	stream := &domain.Stream{
		ID:          event.VideoID,
		Title:       event.Title,
		ChannelID:   channelID,
		ChannelName: channelName,
		Status:      ys.mapEventStatus(event.Status),
	}
	stream.Thumbnail = scrapedEventThumbnail(event)
	stream.Link = stringPtr(fmt.Sprintf("https://www.youtube.com/watch?v=%s", event.VideoID))
	stream.StartScheduled = scrapedEventStartTime(event)
	return stream
}

func scrapedEventThumbnail(event *scraper.UpcomingEvent) *string {
	if len(event.Thumbnail) > 0 {
		return stringPtr(event.Thumbnail[len(event.Thumbnail)-1].URL)
	}
	return stringPtr(fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", event.VideoID))
}

func scrapedEventStartTime(event *scraper.UpcomingEvent) *time.Time {
	if event.StartTime == nil {
		return nil
	}
	startTime := time.Unix(*event.StartTime, 0)
	return &startTime
}

func stringPtr(value string) *string {
	return &value
}

func (ys *serviceImpl) mapEventStatus(status string) domain.StreamStatus {
	switch status {
	case "LIVE":
		return domain.StreamStatusLive
	case "UPCOMING":
		return domain.StreamStatusUpcoming
	default:
		return domain.StreamStatusUpcoming
	}
}
