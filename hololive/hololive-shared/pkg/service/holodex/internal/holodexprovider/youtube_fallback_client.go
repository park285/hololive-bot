package holodexprovider

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func (s *ScraperService) fetchFromYouTubeScraper(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	var (
		events []*scraper.UpcomingEvent
		err    error
	)

	switch {
	case s.fetchUpcoming != nil:
		events, err = s.fetchUpcoming(ctx, channelID)
	case s.youtubeScraper != nil:
		events, err = s.youtubeScraper.GetUpcomingEvents(ctx, channelID)
	default:
		return nil, fmt.Errorf("youtube scraper not configured")
	}
	if err != nil {
		return nil, fmt.Errorf("youtube scraper error: %w", err)
	}

	streams := make([]*domain.Stream, 0, len(events))
	for _, event := range events {
		streams = append(streams, s.convertEventToStream(event, channelID))
	}
	return streams, nil
}
