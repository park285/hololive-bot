package majorevent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCanonicalEventLinkKey(t *testing.T) {
	tests := []struct {
		name string
		link string
		want string
	}{
		{
			name: "jp news link",
			link: "https://hololive.hololivepro.com/news/20260206-02-79/",
			want: "hololive.hololivepro.com/news/20260206-02-79",
		},
		{
			name: "en news link normalized to same key",
			link: "https://hololive.hololivepro.com/en/news/20260206-02-79/",
			want: "hololive.hololivepro.com/news/20260206-02-79",
		},
		{
			name: "empty link",
			link: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalEventLinkKey(tt.link)
			if got != tt.want {
				t.Errorf("canonicalEventLinkKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDedupeEventsByCanonicalLink(t *testing.T) {
	events := []*domain.MajorEvent{
		{
			ExternalID: "https://hololive.hololivepro.com/news/20260206-02-79/",
			Link:       "https://hololive.hololivepro.com/news/20260206-02-79/",
			Title:      "JP version",
		},
		{
			ExternalID: "https://hololive.hololivepro.com/en/news/20260206-02-79/",
			Link:       "https://hololive.hololivepro.com/en/news/20260206-02-79/",
			Title:      "EN version",
		},
		{
			ExternalID: "https://hololive.hololivepro.com/en/news/20260123-01-188/",
			Link:       "https://hololive.hololivepro.com/en/news/20260123-01-188/",
			Title:      "EN only",
		},
	}

	got := dedupeEventsByCanonicalLink(events)
	if len(got) != 2 {
		t.Fatalf("dedupeEventsByCanonicalLink() len = %d, want 2", len(got))
	}
	if got[0].Title != "JP version" {
		t.Errorf("expected first event to keep JP version, got %q", got[0].Title)
	}
}

func TestApplyFallbackEventDate(t *testing.T) {
	pub := time.Date(2026, 2, 16, 15, 4, 5, 0, time.UTC)
	event := &domain.MajorEvent{
		PubDate: &pub,
	}

	applyFallbackEventDate(event)

	if event.EventStartDate == nil || event.EventEndDate == nil {
		t.Fatal("expected fallback start/end dates to be set")
	}
	if event.EventStartDate.Year() != 2026 || event.EventStartDate.Month() != time.February || event.EventStartDate.Day() != 16 {
		t.Fatalf("unexpected fallback start date: %v", event.EventStartDate)
	}
	if !event.EventStartDate.Equal(*event.EventEndDate) {
		t.Fatalf("expected start/end to be equal, got start=%v end=%v", event.EventStartDate, event.EventEndDate)
	}
}

func TestIsRetryableRSSError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "504 Gateway Timeout", err: &httpStatusError{code: 504}, want: true},
		{name: "502 Bad Gateway", err: &httpStatusError{code: 502}, want: true},
		{name: "503 Service Unavailable", err: &httpStatusError{code: 503}, want: true},
		{name: "500 not retryable", err: &httpStatusError{code: 500}, want: false},
		{name: "403 not retryable", err: &httpStatusError{code: 403}, want: false},
		{name: "429 not retryable", err: &httpStatusError{code: 429}, want: false},
		{name: "wrapped 504", err: fmt.Errorf("page 18: %w", &httpStatusError{code: 504}), want: true},
		{name: "context.Canceled", err: context.Canceled, want: false},
		{name: "connection reset", err: fmt.Errorf("connection reset by peer"), want: true},
		{name: "http2 timeout", err: fmt.Errorf("http2: timeout awaiting response headers"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableRSSError(tt.err)
			if got != tt.want {
				t.Errorf("isRetryableRSSError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- scrapePage retry 테스트 ---

type mockCountingHTTPClient struct {
	responses      []*http.Response
	errors         []error
	callCount      int
	closeIdleCalls int
	requests       []*http.Request
}

func (m *mockCountingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.requests = append(m.requests, req)
	idx := m.callCount
	m.callCount++
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return nil, fmt.Errorf("unexpected call %d", idx)
}

func (m *mockCountingHTTPClient) CloseIdleConnections() {
	m.closeIdleCalls++
}

func newTestScraper(client *mockCountingHTTPClient) *Scraper {
	return NewScraper(client, nil,
		WithScraperRetryOpts(&retry.RetryOptions{
			MaxAttempts: 3,
			BaseDelay:   0,
			Jitter:      0,
		}),
	)
}

func makeOKResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func make404Response() *http.Response {
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func make500Response() *http.Response {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("error")),
	}
}

func make304Response() *http.Response {
	return &http.Response{
		StatusCode: http.StatusNotModified,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

// 유효한 RSS XML (최소)
const validRSSXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>test</title></channel></rss>`

// item 1개 포함 RSS XML (pagination 테스트용)
const validRSSXMLWithItem = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>test</title>
<item><title>Test Event</title><link>https://example.com/event/1</link>
<pubDate>Wed, 19 Feb 2026 00:00:00 +0000</pubDate></item>
</channel></rss>`

func TestScrapePage_HTTP2TimeoutThenSuccess(t *testing.T) {
	mock := &mockCountingHTTPClient{
		errors: []error{
			fmt.Errorf("http2: timeout awaiting response headers"),
			nil,
		},
		responses: []*http.Response{
			nil,
			makeOKResponse(validRSSXML),
		},
	}
	s := newTestScraper(mock)

	events, err := s.scrapePage(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if events == nil {
		events = []*domain.MajorEvent{}
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", mock.callCount)
	}
	if mock.closeIdleCalls != 1 {
		t.Errorf("expected 1 CloseIdleConnections call, got %d", mock.closeIdleCalls)
	}
	_ = events
}

func TestScrapePage_ContextCanceled_NoRetry(t *testing.T) {
	mock := &mockCountingHTTPClient{
		errors: []error{context.Canceled},
	}
	s := newTestScraper(mock)

	_, err := s.scrapePage(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err == nil {
		t.Fatal("expected error for context.Canceled")
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 call (no retry), got %d", mock.callCount)
	}
	if mock.closeIdleCalls != 0 {
		t.Errorf("expected 0 CloseIdleConnections calls, got %d", mock.closeIdleCalls)
	}
}

func TestScrapePage_NonRetryable500(t *testing.T) {
	mock := &mockCountingHTTPClient{
		responses: []*http.Response{make500Response()},
	}
	s := newTestScraper(mock)

	_, err := s.scrapePage(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 call (no retry for non-transport error), got %d", mock.callCount)
	}
	if mock.closeIdleCalls != 0 {
		t.Errorf("expected 0 CloseIdleConnections calls, got %d", mock.closeIdleCalls)
	}
}

func TestScrapePage_HTTP2Timeout_AllRetriesExhausted(t *testing.T) {
	mock := &mockCountingHTTPClient{
		errors: []error{
			fmt.Errorf("http2: timeout awaiting response headers"),
			fmt.Errorf("http2: timeout awaiting response headers"),
			fmt.Errorf("http2: timeout awaiting response headers"),
		},
	}
	s := newTestScraper(mock)

	_, err := s.scrapePage(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 calls (MaxRetries), got %d", mock.callCount)
	}
	if mock.closeIdleCalls != 2 {
		t.Errorf("expected 2 CloseIdleConnections calls (retries=attempts-1), got %d", mock.closeIdleCalls)
	}
}

func TestScrapePage_404_PaginationEnd(t *testing.T) {
	mock := &mockCountingHTTPClient{
		responses: []*http.Response{make404Response()},
	}
	s := newTestScraper(mock)

	events, err := s.scrapePage(context.Background(), "https://example.com/feed/", 2, domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events for 404, got %d", len(events))
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 call, got %d", mock.callCount)
	}
	if mock.closeIdleCalls != 0 {
		t.Errorf("expected 0 CloseIdleConnections calls, got %d", mock.closeIdleCalls)
	}
}

func makeStatusResponse(code int) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func TestScrapePage_504GatewayTimeoutThenSuccess(t *testing.T) {
	mock := &mockCountingHTTPClient{
		responses: []*http.Response{
			makeStatusResponse(http.StatusGatewayTimeout),
			makeOKResponse(validRSSXML),
		},
	}
	s := newTestScraper(mock)

	events, err := s.scrapePage(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if events == nil {
		events = []*domain.MajorEvent{}
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", mock.callCount)
	}
	// OnRetry는 모든 재시도에서 CloseIdleConnections 호출 (idle 연결 정리는 항상 안전)
	if mock.closeIdleCalls != 1 {
		t.Errorf("expected 1 CloseIdleConnections call, got %d", mock.closeIdleCalls)
	}
}

func TestScrapePage_502BadGatewayAllRetriesExhausted(t *testing.T) {
	mock := &mockCountingHTTPClient{
		responses: []*http.Response{
			makeStatusResponse(http.StatusBadGateway),
			makeStatusResponse(http.StatusBadGateway),
			makeStatusResponse(http.StatusBadGateway),
		},
	}
	s := newTestScraper(mock)

	_, err := s.scrapePage(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err == nil {
		t.Fatal("expected error after exhausting retries on 502")
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 calls (MaxRetries), got %d", mock.callCount)
	}
}

func TestScrapePage_NonRetryable403(t *testing.T) {
	mock := &mockCountingHTTPClient{
		responses: []*http.Response{
			makeStatusResponse(http.StatusForbidden),
		},
	}
	s := newTestScraper(mock)

	_, err := s.scrapePage(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err == nil {
		t.Fatal("expected error for 403 status")
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 call (no retry for 403), got %d", mock.callCount)
	}
}

func TestScrapePageOnce_ConditionalRequestWith304(t *testing.T) {
	mock := &mockCountingHTTPClient{
		responses: []*http.Response{make304Response()},
	}
	s := newTestScraper(mock)

	pageURL := "https://example.com/feed/"
	s.saveFeedMetadata(pageURL, `"etag-1"`, "Wed, 19 Feb 2026 00:00:00 GMT")

	events, err := s.scrapePageOnce(context.Background(), pageURL, 1, domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error for 304, got: %v", err)
	}
	if events != nil {
		t.Fatalf("expected nil events for 304, got %d", len(events))
	}
	if len(mock.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(mock.requests))
	}

	req := mock.requests[0]
	if got := req.Header.Get("If-None-Match"); got != `"etag-1"` {
		t.Errorf("expected If-None-Match header to be set, got %q", got)
	}
	if got := req.Header.Get("If-Modified-Since"); got != "Wed, 19 Feb 2026 00:00:00 GMT" {
		t.Errorf("expected If-Modified-Since header to be set, got %q", got)
	}
}

func TestScrapePageOnce_SaveFeedMetadataOn200(t *testing.T) {
	resp := makeOKResponse(validRSSXML)
	resp.Header = make(http.Header)
	resp.Header.Set("ETag", `"etag-2"`)
	resp.Header.Set("Last-Modified", "Wed, 19 Feb 2026 00:01:00 GMT")

	mock := &mockCountingHTTPClient{
		responses: []*http.Response{resp},
	}
	s := newTestScraper(mock)

	_, err := s.scrapePageOnce(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	metadata, ok := s.getFeedMetadata("https://example.com/feed/")
	if !ok {
		t.Fatal("expected feed metadata to be saved")
	}
	if metadata.eTag != `"etag-2"` {
		t.Errorf("expected ETag to be saved, got %q", metadata.eTag)
	}
	if metadata.lastModified != "Wed, 19 Feb 2026 00:01:00 GMT" {
		t.Errorf("expected Last-Modified to be saved, got %q", metadata.lastModified)
	}
}

// --- scrapeAllPages skip-and-continue + backfill 테스트 ---

// mockPagedHTTPClient: URL의 paged 파라미터에 따라 응답을 분기하는 mock
type mockPagedHTTPClient struct {
	// pageResponses: page번호 → 응답 시퀀스 (호출될 때마다 순서대로 소비)
	pageResponses  map[int][]*http.Response
	pageErrors     map[int][]error
	pageCalls      map[int]int
	closeIdleCalls int
}

func newMockPagedHTTPClient() *mockPagedHTTPClient {
	return &mockPagedHTTPClient{
		pageResponses: make(map[int][]*http.Response),
		pageErrors:    make(map[int][]error),
		pageCalls:     make(map[int]int),
	}
}

func (m *mockPagedHTTPClient) Do(req *http.Request) (*http.Response, error) {
	page := 1
	if p := req.URL.Query().Get("paged"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}

	idx := m.pageCalls[page]
	m.pageCalls[page]++

	if errs, ok := m.pageErrors[page]; ok && idx < len(errs) && errs[idx] != nil {
		return nil, errs[idx]
	}
	if resps, ok := m.pageResponses[page]; ok && idx < len(resps) {
		return resps[idx], nil
	}
	// 기본: 404 (페이지 끝)
	return make404Response(), nil
}

func (m *mockPagedHTTPClient) CloseIdleConnections() {
	m.closeIdleCalls++
}

func TestScrapeAllPages_SkipAndContinue(t *testing.T) {
	// page 1: OK(item), page 2: 504 (retry 소진), page 3: OK(item), page 4: 404 (종료)
	// backfill에서 page 2: OK(item)
	mock := newMockPagedHTTPClient()
	mock.pageResponses[1] = []*http.Response{makeOKResponse(validRSSXMLWithItem)}
	// page 2: 모든 retry 실패 → backfill에서 성공
	mock.pageResponses[2] = []*http.Response{
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
		// backfill 시도
		makeOKResponse(validRSSXMLWithItem),
	}
	mock.pageResponses[3] = []*http.Response{makeOKResponse(validRSSXMLWithItem)}
	// page 4: 404 = pagination end

	s := NewScraper(mock, nil,
		WithScraperRetryOpts(&retry.RetryOptions{
			MaxAttempts: 3,
			BaseDelay:   0,
			Jitter:      0,
		}),
	)

	events, skippedPages, err := s.scrapeAllPages(context.Background(), "https://example.com/feed/", domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// page 1 + page 3 (1차) + page 2 backfill = 3 페이지 → 각 1 event = 3 events
	if len(events) != 3 {
		t.Errorf("expected 3 events (page 1 + page 3 + backfill page 2), got %d", len(events))
	}
	// page 2는 1차에서 3회 retry 실패 + backfill 1회 성공 = 총 4회 호출
	if mock.pageCalls[2] < 4 {
		t.Errorf("expected page 2 to be called at least 4 times (3 retry + 1 backfill), got %d", mock.pageCalls[2])
	}
	if len(skippedPages) != 0 {
		t.Errorf("expected 0 skipped pages after backfill recovery, got %d", len(skippedPages))
	}
}

func TestScrapeAllPages_ConsecutiveFailsStopsPagination(t *testing.T) {
	// page 1: OK(item), page 2: 504, page 3: 504, page 4: 504 → 연속 3회 실패로 중단
	mock := newMockPagedHTTPClient()
	mock.pageResponses[1] = []*http.Response{makeOKResponse(validRSSXMLWithItem)}
	for p := 2; p <= 4; p++ {
		// 각 페이지에서 retry 3회 모두 504
		mock.pageResponses[p] = []*http.Response{
			makeStatusResponse(http.StatusGatewayTimeout),
			makeStatusResponse(http.StatusGatewayTimeout),
			makeStatusResponse(http.StatusGatewayTimeout),
			// backfill도 504
			makeStatusResponse(http.StatusGatewayTimeout),
			makeStatusResponse(http.StatusGatewayTimeout),
			makeStatusResponse(http.StatusGatewayTimeout),
		}
	}
	// page 5는 호출되면 안 됨 (연속 3회 실패 후 중단)
	mock.pageResponses[5] = []*http.Response{makeOKResponse(validRSSXML)}

	s := NewScraper(mock, nil,
		WithScraperRetryOpts(&retry.RetryOptions{
			MaxAttempts: 3,
			BaseDelay:   0,
			Jitter:      0,
		}),
	)

	events, skippedPages, err := s.scrapeAllPages(context.Background(), "https://example.com/feed/", domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	_ = events

	// page 5는 호출되지 않아야 함
	if mock.pageCalls[5] > 0 {
		t.Errorf("expected page 5 not to be called after 3 consecutive fails, got %d calls", mock.pageCalls[5])
	}
	if len(skippedPages) != 3 {
		t.Errorf("expected 3 unrecovered skipped pages, got %d", len(skippedPages))
		return
	}
	if skippedPages[0] != 2 || skippedPages[1] != 3 || skippedPages[2] != 4 {
		t.Errorf("unexpected skipped pages: %v", skippedPages)
	}
}

func TestScrapeAllPages_Page1Failure_ReturnsFeedError(t *testing.T) {
	mock := newMockPagedHTTPClient()
	// page 1: 모든 retry 실패
	mock.pageResponses[1] = []*http.Response{
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
	}

	s := NewScraper(mock, nil,
		WithScraperRetryOpts(&retry.RetryOptions{
			MaxAttempts: 3,
			BaseDelay:   0,
			Jitter:      0,
		}),
	)

	_, _, err := s.scrapeAllPages(context.Background(), "https://example.com/feed/", domain.MajorEventTypeEvent)
	if err == nil {
		t.Fatal("expected error when page 1 fails")
	}
}

func TestScrapeAllPages_IncrementalStopOnKnownPage(t *testing.T) {
	mock := newMockPagedHTTPClient()
	mock.pageResponses[1] = []*http.Response{makeOKResponse(validRSSXMLWithItem)}
	mock.pageResponses[2] = []*http.Response{makeOKResponse(validRSSXMLWithItem)}

	s := NewScraper(mock, nil,
		WithScraperRetryOpts(&retry.RetryOptions{
			MaxAttempts: 3,
			BaseDelay:   0,
			Jitter:      0,
		}),
	)
	s.loadIncrementalCursor = func(_ context.Context, _ domain.MajorEventType) (*incrementalCursor, error) {
		return &incrementalCursor{
			knownExternalIDs: map[string]struct{}{
				"https://example.com/event/1": {},
			},
		}, nil
	}

	events, skippedPages, err := s.scrapeAllPages(context.Background(), "https://example.com/feed/", domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events due to incremental stop, got %d", len(events))
	}
	if len(skippedPages) != 0 {
		t.Fatalf("expected 0 skipped pages, got %d", len(skippedPages))
	}
	if mock.pageCalls[2] != 0 {
		t.Fatalf("expected page 2 not to be called after incremental stop, got %d", mock.pageCalls[2])
	}
}

func TestShouldStopIncrementalScan_KnownCanonicalLink(t *testing.T) {
	pub := time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC)
	events := []*domain.MajorEvent{
		{
			ExternalID: "https://hololive.hololivepro.com/en/news/20260206-02-79/",
			Link:       "https://hololive.hololivepro.com/en/news/20260206-02-79/",
			PubDate:    &pub,
		},
	}
	cursor := &incrementalCursor{
		knownCanonicalLink: map[string]struct{}{
			"hololive.hololivepro.com/news/20260206-02-79": {},
		},
	}

	if !shouldStopIncrementalScan(events, cursor) {
		t.Fatal("expected incremental scan to stop for known canonical link")
	}
}

func TestScrapeAllPages_NewsTransientFailureContinuesPagination(t *testing.T) {
	mock := newMockPagedHTTPClient()
	mock.pageResponses[1] = []*http.Response{makeOKResponse(validRSSXMLWithItem)}
	mock.pageResponses[2] = []*http.Response{
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
		makeStatusResponse(http.StatusGatewayTimeout),
	}
	mock.pageResponses[3] = []*http.Response{makeOKResponse(validRSSXMLWithItem)}

	s := NewScraper(mock, nil,
		WithScraperRetryOpts(&retry.RetryOptions{
			MaxAttempts: 3,
			BaseDelay:   0,
			Jitter:      0,
		}),
	)

	events, skippedPages, err := s.scrapeAllPages(context.Background(), "https://example.com/feed/", domain.MajorEventTypeNews)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected events from page 1 and page 3, got %d", len(events))
	}
	if mock.pageCalls[3] == 0 {
		t.Fatalf("expected page 3 to be called after page 2 transient failure")
	}
	if len(skippedPages) != 1 || skippedPages[0] != 2 {
		t.Fatalf("expected page 2 to remain skipped after backfill failures, got %v", skippedPages)
	}
}

func TestScrapePage_ConnectionResetThenSuccess(t *testing.T) {
	mock := &mockCountingHTTPClient{
		errors: []error{
			fmt.Errorf("connection reset by peer"),
			nil,
		},
		responses: []*http.Response{
			nil,
			makeOKResponse(validRSSXML),
		},
	}
	s := newTestScraper(mock)

	events, err := s.scrapePage(context.Background(), "https://example.com/feed/", 1, domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if events == nil {
		events = []*domain.MajorEvent{}
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", mock.callCount)
	}
	if mock.closeIdleCalls != 1 {
		t.Errorf("expected 1 CloseIdleConnections call, got %d", mock.closeIdleCalls)
	}
}

type mockURLStatusHTTPClient struct {
	statusByURL map[string]int
	callsByURL  map[string]int
}

func (m *mockURLStatusHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.callsByURL == nil {
		m.callsByURL = make(map[string]int)
	}

	url := req.URL.String()
	m.callsByURL[url]++

	status := http.StatusNotFound
	if configured, ok := m.statusByURL[url]; ok {
		status = configured
	}

	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func TestScrapeAndStore_AllFeedsFail_ReturnsError(t *testing.T) {
	const (
		eventURL  = "https://example.com/events/"
		newsURL   = "https://example.com/news/"
		enNewsURL = "https://example.com/en/news/"
	)

	mock := &mockURLStatusHTTPClient{
		statusByURL: map[string]int{
			eventURL:  http.StatusGatewayTimeout,
			newsURL:   http.StatusGatewayTimeout,
			enNewsURL: http.StatusGatewayTimeout,
		},
	}

	s := NewScraper(mock, nil,
		WithScraperEventURL(eventURL),
		WithScraperRetryOpts(&retry.RetryOptions{
			MaxAttempts: 3,
			BaseDelay:   0,
			Jitter:      0,
		}),
	)
	s.newsURLs = []string{newsURL, enNewsURL}

	stored, err := s.ScrapeAndStore(context.Background())
	if err == nil {
		t.Fatal("expected scrape-all-feeds error, got nil")
	}
	if stored != 0 {
		t.Fatalf("stored = %d, want 0", stored)
	}
	if !strings.Contains(err.Error(), "scrape all feeds") {
		t.Fatalf("expected aggregated scrape error, got %v", err)
	}

	if got := mock.callsByURL[eventURL]; got == 0 {
		t.Fatal("expected event feed to be attempted at least once")
	}
	if got := mock.callsByURL[newsURL]; got == 0 {
		t.Fatal("expected news feed to be attempted at least once")
	}
	if got := mock.callsByURL[enNewsURL]; got == 0 {
		t.Fatal("expected en-news feed to be attempted at least once")
	}
}

func TestScrapeAndStore_PartialFeedFailure_ContinuesWithRemainingFeeds(t *testing.T) {
	const (
		eventURL = "https://example.com/events/"
		newsURL  = "https://example.com/news/"
	)

	mock := &mockURLStatusHTTPClient{
		statusByURL: map[string]int{
			eventURL: http.StatusGatewayTimeout,
			newsURL:  http.StatusNotFound, // empty feed treated as success
		},
	}

	s := NewScraper(mock, &Repository{},
		WithScraperEventURL(eventURL),
		WithScraperRetryOpts(&retry.RetryOptions{
			MaxAttempts: 3,
			BaseDelay:   0,
			Jitter:      0,
		}),
	)
	s.newsURLs = []string{newsURL}
	s.loadIncrementalCursor = func(ctx context.Context, eventType domain.MajorEventType) (*incrementalCursor, error) {
		return nil, nil
	}

	stored, err := s.ScrapeAndStore(context.Background())
	if err != nil {
		t.Fatalf("expected partial failure to return nil error, got %v", err)
	}
	if stored != 0 {
		t.Fatalf("stored = %d, want 0", stored)
	}

	if got := mock.callsByURL[eventURL]; got == 0 {
		t.Fatal("expected failed event feed to be attempted")
	}
	if got := mock.callsByURL[newsURL]; got == 0 {
		t.Fatal("expected remaining feed to still be scraped after event feed failure")
	}
}
