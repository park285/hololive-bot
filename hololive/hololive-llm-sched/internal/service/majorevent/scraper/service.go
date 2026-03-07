// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type eventRepository interface {
	GetRecentExternalIDs(ctx context.Context, eventType domain.MajorEventType, limit int) ([]string, *time.Time, error)
	UpsertEvent(ctx context.Context, event *domain.MajorEvent) error
}

type sourceScrapeResult struct {
	source      FeedSource
	events      []*domain.MajorEvent
	skipped     int
	failed      bool
	failureInfo string
}

// Service는 RSS 수집/파싱/저장을 담당한다.
type Service struct {
	repo    eventRepository
	fetcher *FeedFetcher
	parser  *RSSParser
	config  ServiceConfig
	logger  *slog.Logger
}

// NewService는 Service를 생성한다.
func NewService(
	repo eventRepository,
	fetcher *FeedFetcher,
	parser *RSSParser,
	cfg ServiceConfig,
	logger *slog.Logger,
) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("new scraper service: repository is nil")
	}
	if fetcher == nil {
		return nil, fmt.Errorf("new scraper service: fetcher is nil")
	}
	if parser == nil {
		return nil, fmt.Errorf("new scraper service: parser is nil")
	}

	normalized := normalizeServiceConfig(cfg)
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		repo:    repo,
		fetcher: fetcher,
		parser:  parser,
		config:  normalized,
		logger:  logger,
	}, nil
}

func normalizeServiceConfig(cfg ServiceConfig) ServiceConfig {
	normalized := cfg
	if len(normalized.Sources) == 0 {
		normalized.Sources = DefaultServiceConfig().Sources
	}
	if normalized.FeedConcurrency < 1 {
		normalized.FeedConcurrency = DefaultServiceConfig().FeedConcurrency
	}
	if normalized.IncrementalLimit < 1 {
		normalized.IncrementalLimit = defaultIncrementalMax
	}
	return normalized
}

// Scrape는 전체 피드 소스를 수집/저장한다.
func (s *Service) Scrape(ctx context.Context) (ScrapeResult, error) {
	results := s.scrapeSources(ctx)

	aggregated := ScrapeResult{}
	allEvents := make([]*domain.MajorEvent, 0)
	for _, result := range results {
		aggregated.FeedsAttempted++
		if result.failed {
			aggregated.FeedsFailed++
			s.logger.Warn(
				"Major event feed scrape failed",
				slog.String("source", result.source.Name),
				slog.String("event_type", string(result.source.EventType)),
				slog.String("error", result.failureInfo),
			)
			continue
		}
		aggregated.SkippedKnown += result.skipped
		aggregated.ParsedEvents += len(result.events)
		allEvents = append(allEvents, result.events...)
	}

	if aggregated.FeedsAttempted > 0 && aggregated.FeedsAttempted == aggregated.FeedsFailed {
		return aggregated, fmt.Errorf("scrape feeds: all feeds failed")
	}

	deduped := dedupeEvents(allEvents)
	for _, event := range deduped {
		if err := s.repo.UpsertEvent(ctx, event); err != nil {
			s.logger.Warn(
				"Major event upsert failed",
				slog.String("external_id", event.ExternalID),
				slog.String("error", err.Error()),
			)
			continue
		}
		aggregated.StoredEvents++
	}

	return aggregated, nil
}

func (s *Service) scrapeSources(ctx context.Context) []sourceScrapeResult {
	results := make([]sourceScrapeResult, 0, len(s.config.Sources))
	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(s.config.FeedConcurrency)

	for _, source := range s.config.Sources {
		source := source
		eg.Go(func() error {
			result := s.scrapeSingleSource(egCtx, source)
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
			return nil
		})
	}

	_ = eg.Wait()
	slices.SortFunc(results, func(a, b sourceScrapeResult) int {
		return strings.Compare(a.source.Name, b.source.Name)
	})
	return results
}

func (s *Service) scrapeSingleSource(ctx context.Context, source FeedSource) sourceScrapeResult {
	rawFeed, err := s.fetcher.Fetch(ctx, source.FeedURL)
	if err != nil {
		return sourceScrapeResult{
			source:      source,
			failed:      true,
			failureInfo: fmt.Sprintf("fetch feed: %v", err),
		}
	}

	events, err := s.parser.Parse(rawFeed, source.EventType)
	if err != nil {
		return sourceScrapeResult{
			source:      source,
			failed:      true,
			failureInfo: fmt.Sprintf("parse feed: %v", err),
		}
	}

	filtered, skipped, err := s.filterIncrementalEvents(ctx, source.EventType, events)
	if err != nil {
		return sourceScrapeResult{
			source:      source,
			failed:      true,
			failureInfo: fmt.Sprintf("filter incremental events: %v", err),
		}
	}

	return sourceScrapeResult{
		source:  source,
		events:  filtered,
		skipped: skipped,
	}
}

func (s *Service) filterIncrementalEvents(
	ctx context.Context,
	eventType domain.MajorEventType,
	events []*domain.MajorEvent,
) ([]*domain.MajorEvent, int, error) {
	recentExternalIDs, latestPubDate, err := s.repo.GetRecentExternalIDs(ctx, eventType, s.config.IncrementalLimit)
	if err != nil {
		return nil, 0, fmt.Errorf("filter incremental events: get recent external ids: %w", err)
	}

	knownExternalIDs := make(map[string]struct{}, len(recentExternalIDs))
	for _, externalID := range recentExternalIDs {
		trimmed := strings.TrimSpace(externalID)
		if trimmed == "" {
			continue
		}
		knownExternalIDs[trimmed] = struct{}{}
	}

	latestUTC := time.Time{}
	if latestPubDate != nil {
		latestUTC = latestPubDate.UTC()
	}

	filtered := make([]*domain.MajorEvent, 0, len(events))
	skipped := 0
	for _, event := range events {
		if event == nil {
			continue
		}

		externalID := strings.TrimSpace(event.ExternalID)
		if externalID == "" {
			continue
		}

		if _, exists := knownExternalIDs[externalID]; exists {
			skipped++
			continue
		}

		if !latestUTC.IsZero() && event.PubDate != nil && event.PubDate.Before(latestUTC) {
			skipped++
			continue
		}

		filtered = append(filtered, event)
	}

	return filtered, skipped, nil
}

func dedupeEvents(events []*domain.MajorEvent) []*domain.MajorEvent {
	if len(events) <= 1 {
		return events
	}

	seen := make(map[string]struct{}, len(events))
	deduped := make([]*domain.MajorEvent, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}

		key := strings.TrimSpace(event.ExternalID)
		if key == "" {
			key = strings.TrimSpace(event.Link)
		}
		if key == "" {
			continue
		}

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}
		deduped = append(deduped, event)
	}
	return deduped
}
