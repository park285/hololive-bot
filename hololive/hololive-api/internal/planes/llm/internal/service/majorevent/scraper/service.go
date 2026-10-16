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
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type eventRepository interface {
	GetExistingExternalIDs(ctx context.Context, eventType domain.MajorEventType, externalIDs []string) ([]string, error)
	UpsertEvent(ctx context.Context, event *domain.MajorEvent) error
}

type sourceScrapeResult struct {
	source  FeedSource
	events  []*domain.MajorEvent
	skipped int
	err     error
}

// Service는 RSS 수집/파싱/저장을 담당한다.
type Service struct {
	repository eventRepository
	fetcher    *FeedFetcher
	parser     *RSSParser
	config     ServiceConfig
	logger     *slog.Logger
}

// NewService는 Service를 생성한다.
func NewService(
	repository eventRepository,
	fetcher *FeedFetcher,
	parser *RSSParser,
	config ServiceConfig,
	logger *slog.Logger,
) (*Service, error) {
	if repository == nil {
		return nil, errors.New("new scraper service: repository is nil")
	}

	if fetcher == nil {
		return nil, errors.New("new scraper service: fetcher is nil")
	}

	if parser == nil {
		return nil, errors.New("new scraper service: parser is nil")
	}

	normalized := normalizeServiceConfig(config)

	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		repository: repository,
		fetcher:    fetcher,
		parser:     parser,
		config:     normalized,
		logger:     logger,
	}, nil
}

func normalizeServiceConfig(config ServiceConfig) ServiceConfig {
	normalized := config
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

// Scrape는 예약한 모든 source의 성공·실패를 집계하고 개별 저장 성공을 유지한다.
// 전체 source 실패나 저장 실패는 원인을 보존하며, 저장이 성공한 source 부분 실패만으로 오류를 반환하지 않는다.
func (s *Service) Scrape(ctx context.Context) (ScrapeResult, error) {
	results := s.scrapeSources(ctx)

	aggregated := ScrapeResult{}
	allEvents := make([]*domain.MajorEvent, 0)

	var sourceErrors []error

	for _, result := range results {
		aggregated.FeedsAttempted++

		if result.err != nil {
			aggregated.FeedsFailed++

			sourceErrors = append(sourceErrors, result.err)

			s.logger.Warn(
				"Major event feed scrape failed",
				slog.String("source", result.source.Name),
				slog.String("event_type", string(result.source.EventType)),
				slog.Any("error", result.err),
			)

			continue
		}

		aggregated.SkippedKnown += result.skipped
		aggregated.ParsedEvents += len(result.events)

		allEvents = append(allEvents, result.events...)
	}

	if aggregated.FeedsAttempted > 0 && aggregated.FeedsAttempted == aggregated.FeedsFailed {
		return aggregated, fmt.Errorf("scrape feeds: all feeds failed: %w", errors.Join(sourceErrors...))
	}

	deduped := dedupeEvents(allEvents)

	var persistenceErrors []error

	for _, event := range deduped {
		if err := s.repository.UpsertEvent(ctx, event); err != nil {
			aggregated.FailedEvents++

			persistenceErrors = append(persistenceErrors, fmt.Errorf("persist major event %q: %w", event.ExternalID, err))
			s.logger.Warn(
				"Major event upsert failed",
				slog.String("external_id", event.ExternalID),
				slog.Any("error", err),
			)

			continue
		}

		aggregated.StoredEvents++
	}

	// 부분 성공만으로 새 retry를 만들지 않되, 저장 실패로 반환하는 오류에는 source 원인도 보존한다.
	if len(persistenceErrors) > 0 {
		return aggregated, errors.Join(append(sourceErrors, persistenceErrors...)...)
	}

	return aggregated, nil
}

func (s *Service) scrapeSources(ctx context.Context) []sourceScrapeResult {
	results := make([]sourceScrapeResult, len(s.config.Sources))

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(s.config.FeedConcurrency)

	for i, source := range s.config.Sources {
		eg.Go(func() error {
			result := sourceScrapeResult{source: source}

			guardErr := panicguard.RunE(s.logger, panicguard.BackgroundTask, "major-event-source-scrape", func() error {
				result = s.scrapeSingleSource(egCtx, source)

				return nil
			})
			if guardErr != nil {
				result.err = guardErr
			}

			if result.err != nil {
				result.err = fmt.Errorf("scrape source %q: %w", source.Name, result.err)
			}

			// guard 복구 뒤에도 예약한 source의 결과를 남긴다. 일반 source 실패는 sibling을 취소하지 않는다.
			results[i] = result

			if guardErr != nil {
				return result.err
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil && s.logger != nil {
		s.logger.Warn("major event source scrape group failed", slog.Any("error", err))
	}

	slices.SortFunc(results, func(a, b sourceScrapeResult) int {
		return strings.Compare(a.source.Name, b.source.Name)
	})

	return results
}

func (s *Service) scrapeSingleSource(ctx context.Context, source FeedSource) sourceScrapeResult {
	rawFeed, err := s.fetcher.Fetch(ctx, source.FeedURL)
	if err != nil {
		return sourceScrapeResult{
			source: source,
			err:    fmt.Errorf("fetch feed: %w", err),
		}
	}

	events, err := s.parser.Parse(rawFeed, source.EventType)
	if err != nil {
		return sourceScrapeResult{
			source: source,
			err:    fmt.Errorf("parse feed: %w", err),
		}
	}

	filtered, skipped, err := s.filterIncrementalEvents(ctx, source.EventType, events)
	if err != nil {
		return sourceScrapeResult{
			source: source,
			err:    fmt.Errorf("filter incremental events: %w", err),
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
	filtered := make([]*domain.MajorEvent, 0, len(events))
	skipped := 0

	for chunk := range slices.Chunk(events, s.config.IncrementalLimit) {
		externalIDs := make([]string, 0, len(chunk))
		for _, event := range chunk {
			if event != nil && strings.TrimSpace(event.ExternalID) != "" {
				externalIDs = append(externalIDs, strings.TrimSpace(event.ExternalID))
			}
		}

		if len(externalIDs) == 0 {
			continue
		}

		knownIDs, err := s.repository.GetExistingExternalIDs(ctx, eventType, externalIDs)
		if err != nil {
			return nil, 0, fmt.Errorf("filter incremental events: get existing external ids: %w", err)
		}

		known := knownExternalIDSet(knownIDs)

		for _, event := range chunk {
			keep, skipKnown := filterIncrementalEvent(event, known)
			if keep {
				filtered = append(filtered, event)
			} else if skipKnown {
				skipped++
			}
		}
	}

	return filtered, skipped, nil
}

func knownExternalIDSet(externalIDs []string) map[string]struct{} {
	knownExternalIDs := make(map[string]struct{}, len(externalIDs))
	for _, externalID := range externalIDs {
		trimmed := strings.TrimSpace(externalID)
		if trimmed == "" {
			continue
		}

		knownExternalIDs[trimmed] = struct{}{}
	}

	return knownExternalIDs
}

func filterIncrementalEvent(event *domain.MajorEvent, knownExternalIDs map[string]struct{}) (keep, skipKnown bool) {
	if event == nil {
		return false, false
	}

	externalID := strings.TrimSpace(event.ExternalID)
	if externalID == "" {
		return false, false
	}

	if _, exists := knownExternalIDs[externalID]; exists {
		return false, true
	}

	return true, false
}

func dedupeEvents(events []*domain.MajorEvent) []*domain.MajorEvent {
	if len(events) <= 1 {
		return events
	}

	seen := make(map[string]struct{}, len(events))
	deduped := make([]*domain.MajorEvent, 0, len(events))

	for _, event := range events {
		deduped = appendDedupeEvent(deduped, seen, event)
	}

	return deduped
}

func appendDedupeEvent(
	deduped []*domain.MajorEvent,
	seen map[string]struct{},
	event *domain.MajorEvent,
) []*domain.MajorEvent {
	key, ok := dedupeEventKey(event)
	if !ok {
		return deduped
	}

	if _, exists := seen[key]; exists {
		return deduped
	}

	seen[key] = struct{}{}

	return append(deduped, event)
}

func dedupeEventKey(event *domain.MajorEvent) (string, bool) {
	if event == nil {
		return "", false
	}

	key := strings.TrimSpace(event.ExternalID)
	if key == "" {
		key = strings.TrimSpace(event.Link)
	}

	if key == "" {
		return "", false
	}

	return key, true
}
