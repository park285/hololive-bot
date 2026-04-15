package holodex

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (s *ScraperService) fetchAllStreams(ctx context.Context) ([]*domain.Stream, error) {
	if cached, ok := s.getOfficialPageCache(); ok {
		s.logger.Debug("Official schedule page cache hit",
			slog.String("key", officialSchedulePageCacheKey),
			slog.Int("streams", len(cached)))
		observeOfficialScheduleFallback("official_schedule_page", "hit", officialScheduleFallbackReasonMatched)
		return cached, nil
	}

	result, err, shared := s.officialGroup.Do(officialSchedulePageCacheKey, func() (any, error) {
		if cached, ok := s.getOfficialPageCache(); ok {
			return cached, nil
		}

		streams, fetchErr := s.fetchAllStreamsFromOrigin(ctx)
		if fetchErr != nil {
			return nil, fetchErr
		}

		s.setOfficialPageCache(streams)
		return streams, nil
	})
	if err != nil {
		observeOfficialScheduleFallback("official_schedule_page", "error", classifyOfficialScheduleFallbackReason(err, 0))
		return nil, fmt.Errorf("load official schedule page: %w", err)
	}

	streams, ok := result.([]*domain.Stream)
	if !ok {
		return nil, fmt.Errorf("invalid official schedule page cache result: %T", result)
	}
	if shared {
		s.logger.Debug("Official schedule page request deduplicated",
			slog.String("key", officialSchedulePageCacheKey),
			slog.Int("streams", len(streams)))
	}
	observeOfficialScheduleFallback("official_schedule_page", "hit", classifyOfficialScheduleFallbackReason(nil, len(streams)))
	return cloneStreams(streams), nil
}

func (s *ScraperService) fetchAllStreamsFromOrigin(ctx context.Context) ([]*domain.Stream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/lives/hololive", http.NoBody)
	if err != nil {
		return nil, wrapOfficialScheduleError(officialScheduleFallbackReasonUnknown, fmt.Errorf("failed to create scraper request: %w", err))
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HololiveBot/1.0)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, wrapOfficialScheduleError(officialScheduleFallbackReasonNetwork, fmt.Errorf("HTTP request failed: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, wrapOfficialScheduleError(officialScheduleFallbackReasonNetwork, fmt.Errorf("unexpected status code: %d", resp.StatusCode))
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, wrapOfficialScheduleError(officialScheduleFallbackReasonParse, fmt.Errorf("HTML parse failed: %w", err))
	}

	streams := make([]*domain.Stream, 0)
	parseErrors := 0
	currentDate := ""

	doc.Find(".container .col-12").Each(func(i int, container *goquery.Selection) {
		dateHeader := container.Find(".navbar-inverse .holodule.navbar-text")
		if dateHeader.Length() > 0 {
			dateText := stringutil.TrimSpace(dateHeader.Text())
			dateText = strings.Split(dateText, "(")[0]
			currentDate = stringutil.TrimSpace(dateText)
			s.logger.Debug("Found date section", slog.String("date", currentDate))
			return
		}

		container.Find("a.thumbnail").Each(func(j int, sel *goquery.Selection) {
			stream, err := s.parseStreamElement(sel, currentDate)
			if err != nil {
				parseErrors++
				s.logger.Debug("Failed to parse stream element",
					slog.String("date", currentDate),
					slog.Any("error", err))
				return
			}
			if stream != nil {
				streams = append(streams, stream)
			}
		})
	})

	if len(streams) == 0 {
		return nil, &StructureChangedError{
			Message:     "No streams found - HTML structure may have changed",
			ParseErrors: parseErrors,
		}
	}

	if parseErrors > len(streams)/2 {
		s.logger.Warn("High parse error rate detected",
			slog.Int("successes", len(streams)),
			slog.Int("errors", parseErrors))
	}

	s.logger.Info("Scraper fetched all streams",
		slog.Int("total", len(streams)),
		slog.Int("parse_errors", parseErrors))

	return streams, nil
}

func (s *ScraperService) getOfficialPageCache() ([]*domain.Stream, bool) {
	ttl := constants.OfficialScheduleConfig.PageCacheTTL
	if ttl <= 0 {
		return nil, false
	}

	now := s.now()
	s.officialPageMu.RLock()
	defer s.officialPageMu.RUnlock()

	if s.officialPage.expiresAt.IsZero() || !now.Before(s.officialPage.expiresAt) {
		return nil, false
	}
	return cloneStreams(s.officialPage.streams), true
}

func (s *ScraperService) setOfficialPageCache(streams []*domain.Stream) {
	ttl := constants.OfficialScheduleConfig.PageCacheTTL
	if ttl <= 0 {
		return
	}

	s.officialPageMu.Lock()
	defer s.officialPageMu.Unlock()

	s.officialPage = officialSchedulePageCache{
		streams:   cloneStreams(streams),
		expiresAt: s.now().Add(ttl),
	}
}

func (s *ScraperService) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func cloneStreams(streams []*domain.Stream) []*domain.Stream {
	if streams == nil {
		return nil
	}

	cloned := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		cloned = append(cloned, cloneStream(stream))
	}
	return cloned
}

func cloneStream(stream *domain.Stream) *domain.Stream {
	if stream == nil {
		return nil
	}

	cloned := *stream
	if stream.StartScheduled != nil {
		startScheduled := *stream.StartScheduled
		cloned.StartScheduled = &startScheduled
	}
	if stream.StartActual != nil {
		startActual := *stream.StartActual
		cloned.StartActual = &startActual
	}
	if stream.Duration != nil {
		duration := *stream.Duration
		cloned.Duration = &duration
	}
	if stream.Thumbnail != nil {
		thumbnail := *stream.Thumbnail
		cloned.Thumbnail = &thumbnail
	}
	if stream.Link != nil {
		link := *stream.Link
		cloned.Link = &link
	}
	if stream.TopicID != nil {
		topicID := *stream.TopicID
		cloned.TopicID = &topicID
	}
	if stream.Channel != nil {
		channel := *stream.Channel
		cloned.Channel = &channel
	}
	if stream.ViewerCount != nil {
		viewerCount := *stream.ViewerCount
		cloned.ViewerCount = &viewerCount
	}
	return &cloned
}
