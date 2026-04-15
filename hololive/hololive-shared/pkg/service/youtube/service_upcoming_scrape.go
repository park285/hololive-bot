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
	channelName := ys.getChannelName(channelID)
	if channelName == "" {
		channelName = events[0].ChannelTitle
	}

	for _, event := range events {
		if event.Status != "LIVE" && event.Status != "UPCOMING" {
			continue
		}

		stream := &domain.Stream{
			ID:          event.VideoID,
			Title:       event.Title,
			ChannelID:   channelID,
			ChannelName: channelName,
			Status:      ys.mapEventStatus(event.Status),
		}

		if len(event.Thumbnail) > 0 {
			thumbURL := event.Thumbnail[len(event.Thumbnail)-1].URL
			stream.Thumbnail = &thumbURL
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

		streams = append(streams, stream)
	}

	return streams
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
