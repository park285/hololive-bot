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
	doc, err := s.loadOfficialScheduleDocument(ctx)
	if err != nil {
		return nil, err
	}

	streams, parseErrors := s.parseOfficialScheduleDocument(doc)
	if len(streams) == 0 {
		return nil, &StructureChangedError{
			Message:     "No streams found - HTML structure may have changed",
			ParseErrors: parseErrors,
		}
	}

	s.logOfficialScheduleParseSummary(streams, parseErrors)
	return streams, nil
}

func (s *ScraperService) loadOfficialScheduleDocument(ctx context.Context) (*goquery.Document, error) {
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
	return doc, nil
}

func (s *ScraperService) parseOfficialScheduleDocument(doc *goquery.Document) ([]*domain.Stream, int) {
	streams := make([]*domain.Stream, 0)
	parseErrors := 0
	currentDate := ""

	doc.Find(".container .col-12").Each(func(i int, container *goquery.Selection) {
		dateHeader := container.Find(".navbar-inverse .holodule.navbar-text")
		if dateHeader.Length() > 0 {
			currentDate = officialScheduleDateText(dateHeader)
			s.logger.Debug("Found date section", slog.String("date", currentDate))
			return
		}

		streams, parseErrors = s.appendOfficialScheduleContainerStreams(streams, parseErrors, container, currentDate)
	})

	return streams, parseErrors
}

func officialScheduleDateText(dateHeader *goquery.Selection) string {
	dateText := stringutil.TrimSpace(dateHeader.Text())
	dateText = strings.Split(dateText, "(")[0]
	return stringutil.TrimSpace(dateText)
}

func (s *ScraperService) appendOfficialScheduleContainerStreams(
	streams []*domain.Stream,
	parseErrors int,
	container *goquery.Selection,
	currentDate string,
) ([]*domain.Stream, int) {
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
	return streams, parseErrors
}

func (s *ScraperService) logOfficialScheduleParseSummary(streams []*domain.Stream, parseErrors int) {
	if parseErrors > len(streams)/2 {
		s.logger.Warn("High parse error rate detected",
			slog.Int("successes", len(streams)),
			slog.Int("errors", parseErrors))
	}

	s.logger.Info("Scraper fetched all streams",
		slog.Int("total", len(streams)),
		slog.Int("parse_errors", parseErrors))
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
	cloned.StartScheduled = cloneTimePtr(stream.StartScheduled)
	cloned.StartActual = cloneTimePtr(stream.StartActual)
	cloned.Duration = cloneIntPtr(stream.Duration)
	cloned.Thumbnail = cloneStringPtr(stream.Thumbnail)
	cloned.Link = cloneStringPtr(stream.Link)
	cloned.TopicID = cloneStringPtr(stream.TopicID)
	cloned.Channel = cloneChannelPtr(stream.Channel)
	cloned.ViewerCount = cloneIntPtr(stream.ViewerCount)
	return &cloned
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneChannelPtr(value *domain.Channel) *domain.Channel {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
