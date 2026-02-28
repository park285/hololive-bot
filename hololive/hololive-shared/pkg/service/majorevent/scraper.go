package majorevent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/ctxutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/retry"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Scraper struct {
	httpClient            HTTPClient
	rssParser             *RSSParser
	dateExtractor         *DateExtractor
	repository            *Repository
	eventURL              string
	newsURLs              []string
	logger                *slog.Logger
	retryOpts             *retry.RetryOptions
	loadIncrementalCursor func(ctx context.Context, eventType domain.MajorEventType) (*incrementalCursor, error)
	feedMetadataMu        sync.RWMutex
	feedMetadataByPageURL map[string]feedMetadata
	upsertConcurrency     int
}

type incrementalCursor struct {
	knownExternalIDs   map[string]struct{}
	knownCanonicalLink map[string]struct{}
	latestPubDate      *time.Time
}

type feedMetadata struct {
	eTag         string
	lastModified string
}

type ScraperOption func(*Scraper)

func WithScraperEventURL(eventURL string) ScraperOption {
	return func(s *Scraper) {
		s.eventURL = eventURL
	}
}

func WithScraperLogger(logger *slog.Logger) ScraperOption {
	return func(s *Scraper) {
		s.logger = logger
	}
}

func WithScraperRetryOpts(opts *retry.RetryOptions) ScraperOption {
	return func(s *Scraper) {
		s.retryOpts = opts
	}
}

func NewScraper(httpClient HTTPClient, repository *Repository, opts ...ScraperOption) *Scraper {
	s := &Scraper{
		httpClient:    httpClient,
		rssParser:     NewRSSParser(),
		dateExtractor: NewDateExtractor(),
		repository:    repository,
		eventURL:      constants.MajorEventConfig.EventRSSURL,
		newsURLs: []string{
			constants.MajorEventConfig.NewsRSSURL,
			constants.MajorEventConfig.NewsRSSURLEn,
		},
		logger:                slog.Default(),
		feedMetadataByPageURL: make(map[string]feedMetadata),
		upsertConcurrency:     constants.MajorEventConfig.ScrapeUpsertConcurrency,
	}

	for _, opt := range opts {
		opt(s)
	}

	// 기본 retry 옵션 (WithScraperRetryOpts로 override 가능)
	if s.retryOpts == nil {
		s.retryOpts = &retry.RetryOptions{
			MaxAttempts: constants.MajorEventConfig.MaxRetries,
			BaseDelay:   constants.MajorEventConfig.RetryDelay,
			Jitter:      constants.MajorEventConfig.RetryDelay / 2,
		}
	}
	if s.loadIncrementalCursor == nil {
		s.loadIncrementalCursor = s.loadIncrementalCursorFromRepository
	}

	return s
}

func (s *Scraper) ScrapeAndStore(ctx context.Context) (int, error) {
	type feedSource struct {
		name     string
		feedType domain.MajorEventType
		url      string
	}

	sources := make([]feedSource, 0, 1+len(s.newsURLs))
	sources = append(sources,
		feedSource{name: "event", feedType: domain.MajorEventTypeEvent, url: s.eventURL},
	)
	for i, newsURL := range s.newsURLs {
		name := "news"
		if i == 1 {
			name = "en-news"
		}
		sources = append(sources, feedSource{name: name, feedType: domain.MajorEventTypeNews, url: newsURL})
	}

	var allEvents []*domain.MajorEvent
	var scrapeErr error
	totalSkippedPages := 0
	attemptedFeeds := 0
	failedFeeds := 0

	for _, source := range sources {
		sourceURL := strings.TrimSpace(source.url)
		if sourceURL == "" {
			continue
		}
		attemptedFeeds++

		events, skippedPages, err := s.scrapeAllPages(ctx, sourceURL, source.feedType)
		if err != nil {
			failedFeeds++
			wrapped := fmt.Errorf("%s feed(%s) %s: %w", source.name, source.feedType, sourceURL, err)
			scrapeErr = errors.Join(scrapeErr, wrapped)
			s.logger.Warn("scrape failed",
				slog.String("feed_name", source.name),
				slog.String("feed", string(source.feedType)),
				slog.String("url", sourceURL),
				slog.String("error", wrapped.Error()))
			continue
		}

		allEvents = append(allEvents, events...)
		totalSkippedPages += len(skippedPages)
	}

	if attemptedFeeds > 0 && failedFeeds == attemptedFeeds && scrapeErr != nil {
		return 0, fmt.Errorf("scrape all feeds: %w", scrapeErr)
	}

	allEvents = dedupeEventsByCanonicalLink(allEvents)

	if s.repository == nil {
		return 0, errors.New("scraper repository is nil")
	}

	stored := s.storeEvents(ctx, allEvents)

	s.logger.Info("scrape completed",
		slog.Int("total_scraped", len(allEvents)),
		slog.Int("stored", stored),
		slog.Int("skipped_pages", totalSkippedPages))

	return stored, nil
}

func (s *Scraper) storeEvents(ctx context.Context, events []*domain.MajorEvent) int {
	concurrency := s.upsertConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	var stored atomic.Int64
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, event := range events {
		if event == nil {
			continue
		}

		event.SetEventDatesFromParsed()
		applyFallbackEventDate(event)

		evt := event
		g.Go(func() error {
			if err := s.repository.UpsertEvent(gctx, evt); err != nil {
				s.logger.Error("upsert event failed",
					slog.String("title", evt.Title),
					slog.String("type", string(evt.Type)),
					slog.String("error", err.Error()))
				return nil
			}

			stored.Add(1)
			return nil
		})
	}

	_ = g.Wait()
	return int(stored.Load())
}

func applyFallbackEventDate(event *domain.MajorEvent) {
	if event == nil {
		return
	}
	if event.EventStartDate != nil {
		if event.EventEndDate == nil {
			event.EventEndDate = event.EventStartDate
		}
		return
	}
	if event.PubDate == nil {
		return
	}

	normalized := time.Date(event.PubDate.Year(), event.PubDate.Month(), event.PubDate.Day(), 0, 0, 0, 0, time.UTC)
	event.EventStartDate = &normalized
	event.EventEndDate = &normalized
}

func (s *Scraper) scrapeAllPages(ctx context.Context, baseURL string, eventType domain.MajorEventType) ([]*domain.MajorEvent, []int, error) {
	var allEvents []*domain.MajorEvent
	maxPages := constants.MajorEventConfig.MaxPages
	loadCursor := s.loadIncrementalCursor
	if loadCursor == nil {
		loadCursor = s.loadIncrementalCursorFromRepository
	}
	cursor, cursorErr := loadCursor(ctx, eventType)
	if cursorErr != nil {
		s.logger.Debug("incremental cursor unavailable, fallback to full scan",
			slog.String("type", string(eventType)),
			slog.String("error", cursorErr.Error()))
		cursor = nil
	}

	// 1차 패스: 실패 페이지는 skip하고 다음 페이지 계속 수집
	var failedPages []int
	var skippedPages []int
	consecutiveFails := 0
	const maxConsecutiveFails = 3 // 연속 3회 실패 시 pagination 종료

	for page := 1; page <= maxPages; page++ {
		events, err := s.scrapePage(ctx, baseURL, page, eventType)
		if err != nil {
			// page 1 실패는 feed 전체 접근 불가 → 즉시 반환
			if page == 1 {
				return nil, nil, fmt.Errorf("scrape first page: %w", err)
			}

			consecutiveFails++
			failedPages = append(failedPages, page)
			s.logger.Debug("scrape page failed, skipping",
				slog.Int("page", page),
				slog.String("type", string(eventType)),
				slog.Int("consecutive_fails", consecutiveFails),
				slog.String("error", err.Error()))

			if consecutiveFails >= maxConsecutiveFails {
				s.logger.Info("too many consecutive failures, stopping pagination",
					slog.Int("page", page),
					slog.String("type", string(eventType)),
					slog.Int("consecutive_fails", consecutiveFails))
				break
			}

			// 실패 후에도 딜레이 적용 (origin 부하 감소)
			if !ctxutil.SleepWithContext(ctx, constants.MajorEventConfig.PageDelay) {
				break
			}
			continue
		}

		consecutiveFails = 0

		if len(events) == 0 {
			break
		}

		if shouldStopIncrementalScan(events, cursor) {
			s.logger.Debug("incremental stop: known page reached",
				slog.Int("page", page),
				slog.String("type", string(eventType)),
				slog.Int("events", len(events)))
			break
		}

		allEvents = append(allEvents, events...)
		s.logger.Debug("scraped page",
			slog.Int("page", page),
			slog.String("type", string(eventType)),
			slog.Int("events", len(events)))

		// 서버 rate-limit 회피를 위한 페이지 간 딜레이
		if page < maxPages {
			if !ctxutil.SleepWithContext(ctx, constants.MajorEventConfig.PageDelay) {
				break
			}
		}
	}

	recoveredEvents, skippedPages := s.backfillFailedPages(ctx, baseURL, eventType, failedPages)
	if len(recoveredEvents) > 0 {
		allEvents = append(allEvents, recoveredEvents...)
	}

	return allEvents, skippedPages, nil
}

func (s *Scraper) backfillFailedPages(ctx context.Context, baseURL string, eventType domain.MajorEventType, failedPages []int) ([]*domain.MajorEvent, []int) {
	if len(failedPages) == 0 {
		return nil, nil
	}

	s.logger.Info("backfill: retrying failed pages",
		slog.String("type", string(eventType)),
		slog.Int("failed_count", len(failedPages)))

	var recoveredEvents []*domain.MajorEvent
	var skippedPages []int

	for _, page := range failedPages {
		if !ctxutil.SleepWithContext(ctx, constants.MajorEventConfig.PageDelay) {
			break
		}

		events, err := s.scrapePage(ctx, baseURL, page, eventType)
		if err != nil {
			s.logger.Debug("backfill page failed",
				slog.Int("page", page),
				slog.String("type", string(eventType)),
				slog.String("error", err.Error()))
			skippedPages = append(skippedPages, page)
			continue
		}

		if len(events) > 0 {
			recoveredEvents = append(recoveredEvents, events...)
			s.logger.Info("backfill page recovered",
				slog.Int("page", page),
				slog.String("type", string(eventType)),
				slog.Int("events", len(events)))
		}
	}

	return recoveredEvents, skippedPages
}

func (s *Scraper) scrapePage(ctx context.Context, baseURL string, page int, eventType domain.MajorEventType) ([]*domain.MajorEvent, error) {
	var result []*domain.MajorEvent

	opts := *s.retryOpts
	opts.ShouldRetry = isRetryableRSSError
	opts.OnRetry = func(attempt int, err error, delay time.Duration) {
		// idle 연결 정리 (zombie 연결 재사용 방지)
		if closer, ok := s.httpClient.(idleConnCloser); ok {
			closer.CloseIdleConnections()
		}
		s.logger.Debug("scrapePage retry",
			slog.Int("attempt", attempt),
			slog.Int("page", page),
			slog.String("type", string(eventType)),
			slog.String("error", err.Error()),
			slog.Duration("delay", delay))
	}

	err := retry.WithRetry(ctx, opts, func(ctx context.Context) error {
		events, fetchErr := s.scrapePageOnce(ctx, baseURL, page, eventType)
		if fetchErr != nil {
			return fetchErr
		}
		result = events
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("scrape page %d: %w", page, err)
	}
	return result, nil
}

func (s *Scraper) scrapePageOnce(ctx context.Context, baseURL string, page int, eventType domain.MajorEventType) ([]*domain.MajorEvent, error) {
	pageURL := baseURL
	if page > 1 {
		pageURL = baseURL + "?paged=" + strconv.Itoa(page)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", constants.MajorEventConfig.UserAgent)
	s.applyConditionalRequestHeaders(req, pageURL)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	s.saveFeedMetadata(pageURL, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"))

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	parsed, err := s.rssParser.ParseWithType(body, eventType)
	if err != nil {
		return nil, fmt.Errorf("parse RSS: %w", err)
	}

	events := make([]*domain.MajorEvent, 0, len(parsed))
	for i := range parsed {
		event := &parsed[i]
		event.EventDates = s.dateExtractor.ExtractEventDates(event.Description)
		events = append(events, event)
	}

	return events, nil
}

func (s *Scraper) loadIncrementalCursorFromRepository(ctx context.Context, eventType domain.MajorEventType) (*incrementalCursor, error) {
	if s.repository == nil {
		return nil, nil
	}

	limit := constants.MajorEventConfig.IncrementalCursorLimit
	if limit <= 0 {
		limit = 1
	}

	externalIDs, latestPubDate, err := s.repository.GetRecentExternalIDs(ctx, eventType, limit)
	if err != nil {
		return nil, fmt.Errorf("get incremental cursor: %w", err)
	}
	if len(externalIDs) == 0 && latestPubDate == nil {
		return nil, nil
	}

	knownExternalIDs := make(map[string]struct{}, len(externalIDs))
	knownCanonicalLink := make(map[string]struct{}, len(externalIDs))
	for _, externalID := range externalIDs {
		normalized := strings.TrimSpace(externalID)
		if normalized == "" {
			continue
		}
		knownExternalIDs[normalized] = struct{}{}
		if key := canonicalEventLinkKey(normalized); key != "" {
			knownCanonicalLink[key] = struct{}{}
		}
	}

	return &incrementalCursor{
		knownExternalIDs:   knownExternalIDs,
		knownCanonicalLink: knownCanonicalLink,
		latestPubDate:      latestPubDate,
	}, nil
}

func shouldStopIncrementalScan(events []*domain.MajorEvent, cursor *incrementalCursor) bool {
	if len(events) == 0 || cursor == nil {
		return false
	}
	if len(cursor.knownExternalIDs) == 0 && len(cursor.knownCanonicalLink) == 0 && cursor.latestPubDate == nil {
		return false
	}

	hasKnownSignal := false

	for _, event := range events {
		if event == nil {
			return false
		}

		knownByExternalID := false
		if externalID := strings.TrimSpace(event.ExternalID); externalID != "" {
			_, knownByExternalID = cursor.knownExternalIDs[externalID]
		}

		knownByCanonicalLink := false
		canonicalKey := canonicalEventLinkKey(event.Link)
		if canonicalKey == "" {
			canonicalKey = canonicalEventLinkKey(event.ExternalID)
		}
		if canonicalKey != "" {
			_, knownByCanonicalLink = cursor.knownCanonicalLink[canonicalKey]
		}

		knownByPubDate := false
		if cursor.latestPubDate != nil && event.PubDate != nil {
			knownByPubDate = event.PubDate.Before(*cursor.latestPubDate)
		}

		if !knownByExternalID && !knownByCanonicalLink && !knownByPubDate {
			return false
		}
		hasKnownSignal = true
	}

	return hasKnownSignal
}

func (s *Scraper) applyConditionalRequestHeaders(req *http.Request, pageURL string) {
	metadata, ok := s.getFeedMetadata(pageURL)
	if !ok {
		return
	}
	if metadata.eTag != "" {
		req.Header.Set("If-None-Match", metadata.eTag)
	}
	if metadata.lastModified != "" {
		req.Header.Set("If-Modified-Since", metadata.lastModified)
	}
}

func (s *Scraper) getFeedMetadata(pageURL string) (feedMetadata, bool) {
	s.feedMetadataMu.RLock()
	defer s.feedMetadataMu.RUnlock()

	metadata, ok := s.feedMetadataByPageURL[pageURL]
	return metadata, ok
}

func (s *Scraper) saveFeedMetadata(pageURL, eTag, lastModified string) {
	normalizedETag := strings.TrimSpace(eTag)
	normalizedLastModified := strings.TrimSpace(lastModified)
	if normalizedETag == "" && normalizedLastModified == "" {
		return
	}

	s.feedMetadataMu.Lock()
	defer s.feedMetadataMu.Unlock()

	metadata := s.feedMetadataByPageURL[pageURL]
	if normalizedETag != "" {
		metadata.eTag = normalizedETag
	}
	if normalizedLastModified != "" {
		metadata.lastModified = normalizedLastModified
	}
	s.feedMetadataByPageURL[pageURL] = metadata
}

func dedupeEventsByCanonicalLink(events []*domain.MajorEvent) []*domain.MajorEvent {
	if len(events) <= 1 {
		return events
	}

	seen := make(map[string]struct{}, len(events))
	deduped := make([]*domain.MajorEvent, 0, len(events))

	for i := range events {
		event := events[i]
		if event == nil {
			continue
		}
		key := canonicalEventLinkKey(event.Link)
		if key == "" {
			key = event.ExternalID
		}
		if key == "" {
			deduped = append(deduped, event)
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

// httpStatusError: HTTP 상태 코드 기반 에러 (재시도 판단용)
type httpStatusError struct {
	code int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("unexpected status code: %d", e.code)
}

// isRetryableHTTPStatus: 재시도 대상 HTTP 상태 코드 (CDN/origin 일시 장애)
func isRetryableHTTPStatus(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// idleConnCloser: transport 수준 idle 연결 정리 인터페이스
type idleConnCloser interface {
	CloseIdleConnections()
}

// isRetryableRSSError: 재시도 가능한 RSS 요청 에러 판별
func isRetryableRSSError(err error) bool {
	if err == nil {
		return false
	}

	// HTTP 상태 코드 기반 판별 (502/503/504 = CDN/origin 일시 장애)
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return isRetryableHTTPStatus(statusErr.code)
	}

	// 호출자 컨텍스트 취소는 재시도하지 않음
	if errors.Is(err, context.Canceled) {
		return false
	}

	// deadline 초과: 시그니처 기반 판별
	if errors.Is(err, context.DeadlineExceeded) {
		return hasRSSTransientSignature(err.Error())
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if isRSSTimeoutOrTemporary(urlErr) {
			return true
		}
		if urlErr.Err != nil {
			if isRSSTimeoutOrTemporary(urlErr.Err) {
				return true
			}
			return hasRSSTransientSignature(urlErr.Err.Error())
		}
		return false
	}

	if isRSSTimeoutOrTemporary(err) {
		return true
	}

	return hasRSSTransientSignature(err.Error())
}

type temporaryError interface {
	Temporary() bool
}

func isRSSTimeoutOrTemporary(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var tempErr temporaryError
	return errors.As(err, &tempErr) && tempErr.Temporary()
}

// hasRSSTransientSignature: 일시 네트워크 에러 시그니처 매칭 (9개 패턴)
func hasRSSTransientSignature(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "connection reset by peer") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "http2: timeout awaiting response headers") ||
		strings.Contains(lower, "timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "client.timeout exceeded while awaiting headers") ||
		strings.Contains(lower, "client.timeout exceeded") ||
		strings.Contains(lower, "unexpected eof")
}

func canonicalEventLinkKey(raw string) string {
	link := strings.TrimSpace(raw)
	if link == "" {
		return ""
	}

	parsed, err := url.Parse(link)
	if err != nil {
		return link
	}

	host := strings.ToLower(parsed.Host)
	path := strings.TrimSpace(parsed.EscapedPath())
	if path == "" {
		path = "/"
	}

	if host == "hololive.hololivepro.com" {
		path = strings.TrimPrefix(path, "/en/")
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}

	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}

	return host + path
}
