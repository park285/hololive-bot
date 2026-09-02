package htmlscraper

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func (s *Service) FetchYouTubeSchedule(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	out, err := s.fetchYouTubeSchedule(ctx, channelID, false)
	if err != nil {
		return out, fmt.Errorf("fetch youtube schedule: %w", err)
	}

	return out, nil
}

func (s *Service) FetchYouTubeScheduleWaitAdmission(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	out, err := s.fetchYouTubeSchedule(ctx, channelID, true)
	if err != nil {
		return out, fmt.Errorf("fetch youtube schedule: %w", err)
	}

	return out, nil
}

func (s *Service) fetchYouTubeSchedule(ctx context.Context, channelID string, waitAdmission bool) ([]*domain.Stream, error) {
	events, err := s.fetchYouTubeEvents(ctx, channelID, waitAdmission)
	if err != nil {
		return nil, fmt.Errorf("youtube scraper error: %w", err)
	}

	return s.convertEventsToStreams(events, channelID), nil
}

func (s *Service) fetchYouTubeEvents(ctx context.Context, channelID string, waitAdmission bool) ([]*parser.UpcomingEvent, error) {
	if s.youtubeClient == nil {
		return nil, errors.New("youtube scraper not configured")
	}

	fetch := s.youtubeClient.GetUpcomingEvents
	operation := "get upcoming events"

	if waitAdmission {
		fetch = s.youtubeClient.GetUpcomingEventsWaitAdmission
		operation = "get upcoming events wait admission"
	}

	out, err := fetch(ctx, channelID)
	if err != nil {
		return out, fmt.Errorf("%s: %w", operation, err)
	}

	return out, nil
}

func (s *Service) convertEventsToStreams(events []*parser.UpcomingEvent, channelID string) []*domain.Stream {
	streams := make([]*domain.Stream, 0, len(events))
	for _, event := range events {
		streams = append(streams, s.convertEventToStream(event, channelID))
	}

	return streams
}
