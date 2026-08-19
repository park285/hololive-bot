package htmlscraper

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func (s *Service) FetchYouTubeSchedule(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	return s.fetchYouTubeSchedule(ctx, channelID, false)
}

func (s *Service) FetchYouTubeScheduleWaitAdmission(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	return s.fetchYouTubeSchedule(ctx, channelID, true)
}

func (s *Service) fetchYouTubeSchedule(ctx context.Context, channelID string, waitAdmission bool) ([]*domain.Stream, error) {
	events, err := s.fetchYouTubeEvents(ctx, channelID, waitAdmission)
	if err != nil {
		return nil, fmt.Errorf("youtube scraper error: %w", err)
	}

	return s.convertEventsToStreams(events, channelID), nil
}

func (s *Service) fetchYouTubeEvents(ctx context.Context, channelID string, waitAdmission bool) ([]*parser.UpcomingEvent, error) {
	switch {
	case s.youtubeClient != nil && waitAdmission:
		return s.youtubeClient.GetUpcomingEventsWaitAdmission(ctx, channelID)
	case s.youtubeClient != nil:
		return s.youtubeClient.GetUpcomingEvents(ctx, channelID)
	default:
		return nil, fmt.Errorf("youtube scraper not configured")
	}
}

func (s *Service) convertEventsToStreams(events []*parser.UpcomingEvent, channelID string) []*domain.Stream {
	streams := make([]*domain.Stream, 0, len(events))
	for _, event := range events {
		streams = append(streams, s.convertEventToStream(event, channelID))
	}
	return streams
}
