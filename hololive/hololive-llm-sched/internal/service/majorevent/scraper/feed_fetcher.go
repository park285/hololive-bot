package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
)

// FeedFetcher는 RSS 원문을 HTTP로 가져온다.
type FeedFetcher struct {
	client     *http.Client
	userAgent  string
	maxBodyLen int64
}

// NewFeedFetcher는 FeedFetcher를 생성한다.
func NewFeedFetcher(client *http.Client, cfg FeedFetcherConfig) *FeedFetcher {
	if client == nil {
		client = httputil.NewExternalAPIClient(defaultFeedHTTPTimeout)
	}

	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultFeedUserAgent
	}

	maxBodyLen := cfg.MaxBodySize
	if maxBodyLen <= 0 {
		maxBodyLen = defaultMaxBodyBytes
	}

	return &FeedFetcher{
		client:     client,
		userAgent:  userAgent,
		maxBodyLen: maxBodyLen,
	}
}

// Fetch는 지정 URL의 RSS 원문을 조회한다.
func (f *FeedFetcher) Fetch(ctx context.Context, feedURL string) ([]byte, error) {
	if f == nil || f.client == nil {
		return nil, fmt.Errorf("fetch feed: fetcher is nil")
	}
	trimmedURL := strings.TrimSpace(feedURL)
	if trimmedURL == "" {
		return nil, fmt.Errorf("fetch feed: feed url is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmedURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch feed: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBodyLen))
	if err != nil {
		return nil, fmt.Errorf("fetch feed: read body: %w", err)
	}
	return body, nil
}
