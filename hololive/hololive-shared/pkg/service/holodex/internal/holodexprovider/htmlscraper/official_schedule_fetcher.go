package htmlscraper

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/park285/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (s *Service) fetchAllStreams(ctx context.Context) ([]*domain.Stream, error) {
	if cached, ok := s.getOfficialPageCache(); ok {
		s.logger.Debug("Official schedule page cache hit",
			slog.String("key", OfficialSchedulePageCacheKey),
			slog.Int("streams", len(cached)))
		observeOfficialScheduleFallback("official_schedule_page", "hit", officialScheduleFallbackReasonMatched)
		return cached, nil
	}

	result, err, shared := s.officialGroup.Do(OfficialSchedulePageCacheKey, func() (any, error) {
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
			slog.String("key", OfficialSchedulePageCacheKey),
			slog.Int("streams", len(streams)))
	}
	observeOfficialScheduleFallback("official_schedule_page", "hit", classifyOfficialScheduleFallbackReason(nil, len(streams)))
	return CloneStreams(streams), nil
}

func (s *Service) fetchAllStreamsFromOrigin(ctx context.Context) ([]*domain.Stream, error) {
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

func (s *Service) loadOfficialScheduleDocument(ctx context.Context) (*goquery.Document, error) {
	req, err := s.newOfficialScheduleRequest(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, wrapOfficialScheduleError(officialScheduleFallbackReasonNetwork, officialScheduleRequestError(err, resp == nil))
	}
	if resp == nil {
		return nil, wrapOfficialScheduleError(officialScheduleFallbackReasonNetwork, fmt.Errorf("HTTP request failed: nil response"))
	}
	if err := validateOfficialScheduleResponse(resp); err != nil {
		return nil, err
	}
	defer s.closeOfficialScheduleResponse(resp)

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, wrapOfficialScheduleError(officialScheduleFallbackReasonParse, fmt.Errorf("HTML parse failed: %w", err))
	}
	return doc, nil
}

func (s *Service) closeOfficialScheduleResponse(resp *http.Response) {
	if closeErr := resp.Body.Close(); closeErr != nil && s.logger != nil {
		s.logger.Warn("Failed to close official schedule response body", "error", closeErr)
	}
}

func (s *Service) newOfficialScheduleRequest(ctx context.Context) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/lives/hololive", http.NoBody)
	if err != nil {
		return nil, wrapOfficialScheduleError(officialScheduleFallbackReasonUnknown, fmt.Errorf("failed to create scraper request: %w", err))
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HololiveBot/1.0)")
	return req, nil
}

func officialScheduleRequestError(err error, nilResponse bool) error {
	if nilResponse {
		err = fmt.Errorf("nil response: %w", err)
	}
	return fmt.Errorf("HTTP request failed: %w", err)
}

func validateOfficialScheduleResponse(resp *http.Response) error {
	if resp == nil {
		return wrapOfficialScheduleError(officialScheduleFallbackReasonNetwork, fmt.Errorf("HTTP request failed: nil response"))
	}
	if resp.Body == nil {
		return wrapOfficialScheduleError(officialScheduleFallbackReasonNetwork, fmt.Errorf("HTTP request failed: nil response body"))
	}
	if resp.StatusCode != http.StatusOK {
		return wrapOfficialScheduleError(officialScheduleFallbackReasonNetwork, fmt.Errorf("unexpected status code: %d", resp.StatusCode))
	}
	return nil
}

func (s *Service) parseOfficialScheduleDocument(doc *goquery.Document) (result0 []*domain.Stream, result1 int) {
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

func (s *Service) appendOfficialScheduleContainerStreams(
	streams []*domain.Stream,
	parseErrors int,
	container *goquery.Selection,
	currentDate string,
) (result0 []*domain.Stream, result1 int) {
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

func (s *Service) logOfficialScheduleParseSummary(streams []*domain.Stream, parseErrors int) {
	if parseErrors > len(streams)/2 {
		s.logger.Warn("High parse error rate detected",
			slog.Int("successes", len(streams)),
			slog.Int("errors", parseErrors))
	}

	s.logger.Info("Scraper fetched all streams",
		slog.Int("total", len(streams)),
		slog.Int("parse_errors", parseErrors))
}

func (s *Service) getOfficialPageCache() ([]*domain.Stream, bool) {
	ttl := config.DefaultOfficialScheduleConfig().PageCacheTTL
	if ttl <= 0 {
		return nil, false
	}

	now := s.now()
	s.officialPageMu.RLock()
	defer s.officialPageMu.RUnlock()

	if s.officialPage.expiresAt.IsZero() || !now.Before(s.officialPage.expiresAt) {
		return nil, false
	}
	return CloneStreams(s.officialPage.streams), true
}

func (s *Service) setOfficialPageCache(streams []*domain.Stream) {
	ttl := config.DefaultOfficialScheduleConfig().PageCacheTTL
	if ttl <= 0 {
		return
	}

	s.officialPageMu.Lock()
	defer s.officialPageMu.Unlock()

	s.officialPage = officialSchedulePageCache{
		streams:   CloneStreams(streams),
		expiresAt: s.now().Add(ttl),
	}
}

func (s *Service) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func CloneStreams(streams []*domain.Stream) []*domain.Stream {
	if streams == nil {
		return nil
	}
	cloned := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		cloned = append(cloned, CloneStream(stream))
	}
	return cloned
}

func CloneStream(stream *domain.Stream) *domain.Stream {
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
