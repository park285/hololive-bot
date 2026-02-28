package majorevent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type scrapeScriptStep struct {
	statusCode int
	body       string
	err        error
}

type scriptedScrapeHTTPClient struct {
	scripts map[string][]scrapeScriptStep
	calls   map[string]int
}

func (m *scriptedScrapeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	idx := m.calls[url]
	m.calls[url] = idx + 1

	steps, ok := m.scripts[url]
	if !ok || idx >= len(steps) {
		return nil, fmt.Errorf("unexpected request: %s (call=%d)", url, idx+1)
	}

	step := steps[idx]
	if step.err != nil {
		return nil, step.err
	}

	return &http.Response{
		StatusCode: step.statusCode,
		Body:       io.NopCloser(strings.NewReader(step.body)),
		Header:     make(http.Header),
	}, nil
}

func TestScrapeAndStore_PartialFeedSuccess_ReturnsNilError(t *testing.T) {
	eventURL := "https://example.com/events/feed/"
	newsURL := "https://example.com/news/feed/"
	newsEnURL := "https://example.com/en/news/feed/"

	client := &scriptedScrapeHTTPClient{
		scripts: map[string][]scrapeScriptStep{
			eventURL: {{statusCode: http.StatusOK, body: validRSSXML}},
			newsURL:  {{err: fmt.Errorf("news feed unavailable")}},
			newsEnURL: {{
				statusCode: http.StatusBadGateway,
				body:       "bad gateway",
			}},
		},
		calls: map[string]int{},
	}

	scraper := NewScraper(client, &Repository{},
		WithScraperEventURL(eventURL),
		WithScraperRetryOpts(&retry.RetryOptions{MaxAttempts: 1, BaseDelay: 0, Jitter: 0}),
	)
	scraper.newsURLs = []string{newsURL, newsEnURL}
	scraper.loadIncrementalCursor = func(_ context.Context, _ domain.MajorEventType) (*incrementalCursor, error) {
		return nil, nil
	}

	stored, err := scraper.ScrapeAndStore(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for partial feed success, got %v", err)
	}
	if stored != 0 {
		t.Fatalf("expected 0 stored events for empty successful feed, got %d", stored)
	}
}

func TestScrapeAndStore_AllFeedsFailed_ReturnsError(t *testing.T) {
	eventURL := "https://example.com/events/feed/"
	newsURL := "https://example.com/news/feed/"
	newsEnURL := "https://example.com/en/news/feed/"

	client := &scriptedScrapeHTTPClient{
		scripts: map[string][]scrapeScriptStep{
			eventURL:  {{err: fmt.Errorf("event feed unavailable")}},
			newsURL:   {{err: fmt.Errorf("news feed unavailable")}},
			newsEnURL: {{err: fmt.Errorf("en news feed unavailable")}},
		},
		calls: map[string]int{},
	}

	scraper := NewScraper(client, nil,
		WithScraperEventURL(eventURL),
		WithScraperRetryOpts(&retry.RetryOptions{MaxAttempts: 1, BaseDelay: 0, Jitter: 0}),
	)
	scraper.newsURLs = []string{newsURL, newsEnURL}

	_, err := scraper.ScrapeAndStore(context.Background())
	if err == nil {
		t.Fatal("expected error when all feeds fail")
	}
	if !strings.Contains(err.Error(), "scrape all feeds") {
		t.Fatalf("expected scrape all feeds error, got %v", err)
	}
}
