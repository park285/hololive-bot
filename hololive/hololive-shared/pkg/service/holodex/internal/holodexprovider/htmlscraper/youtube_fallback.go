package htmlscraper

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func (s *Service) FetchFromYouTubeProducer(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	return s.fetchFromYouTubeProducer(ctx, channelID, false)
}

func (s *Service) FetchFromYouTubeProducerWaitAdmission(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	return s.fetchFromYouTubeProducer(ctx, channelID, true)
}

func (s *Service) fetchFromYouTubeProducer(ctx context.Context, channelID string, waitAdmission bool) ([]*domain.Stream, error) {
	events, err := s.fetchYouTubeProducerEvents(ctx, channelID, waitAdmission)
	if err != nil {
		return nil, fmt.Errorf("youtube producer error: %w", err)
	}

	return s.convertEventsToStreams(events, channelID), nil
}

func (s *Service) fetchYouTubeProducerEvents(ctx context.Context, channelID string, waitAdmission bool) ([]*scraper.UpcomingEvent, error) {
	switch {
	case s.fetchUpcoming != nil:
		return s.fetchUpcoming(ctx, channelID)
	case s.youtubeProducer != nil && waitAdmission:
		return s.youtubeProducer.GetUpcomingEventsWaitAdmission(ctx, channelID)
	case s.youtubeProducer != nil:
		return s.youtubeProducer.GetUpcomingEvents(ctx, channelID)
	default:
		return nil, fmt.Errorf("youtube producer not configured")
	}
}

func (s *Service) convertEventsToStreams(events []*scraper.UpcomingEvent, channelID string) []*domain.Stream {
	streams := make([]*domain.Stream, 0, len(events))
	for _, event := range events {
		streams = append(streams, s.convertEventToStream(event, channelID))
	}
	return streams
}
