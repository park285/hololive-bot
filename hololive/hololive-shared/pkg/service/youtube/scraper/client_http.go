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
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

func (c *Client) currentPageFetcher() pageFetcher {
	netHTTPFetcher := netHTTPPageFetcher{client: c}
	switch normalizeFetcherEngine(c.fetcherEngine) {
	case FetcherEngineGoScrapy:
		return goscrapyPageFetcher{client: c, fallback: netHTTPFetcher}
	case FetcherEngineBrowserSnapshot:
		return netHTTPFetcher
	default:
		return netHTTPFetcher
	}
}

// fetchPageOnce: 단일 HTTP 요청 수행 (재시도 없음)
func (c *Client) fetchPageOnce(ctx context.Context, pageURL string) (string, error) {
	// 불변식: hard cooldown만 차단 (transient는 재시도 허용)
	if cooldownRemaining := c.backoffState.HardCooldownRemaining(); cooldownRemaining > 0 {
		return "", fmt.Errorf("in cooldown for %v: %w", cooldownRemaining.Round(time.Second), ErrRateLimited)
	}

	if err := c.rateLimiter.WaitWithBucket(ctx, distributedBucketFromURL(pageURL)); err != nil {
		return "", fmt.Errorf("rate limiter wait failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// 헤더 스냅샷 기반 설정
	snap := c.uaProvider.Headers(ctx)
	applyScraperHeaders(req, snap)

	resp, err := c.currentPageFetcher().FetchPage(ctx, pageFetchRequest{URL: pageURL, Header: req.Header})
	if err != nil {
		return "", err
	}

	if err := c.handleFetchStatus(pageURL, resp); err != nil {
		return "", err
	}

	c.backoffState.RecordSuccess()
	return string(resp.Body), nil
}

func (c *Client) handleFetchStatus(pageURL string, resp pageFetchResponse) error {
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return c.handleRateLimitedFetch(pageURL, resp.StatusCode, retryAfter)
	case http.StatusForbidden:
		return c.handleForbiddenFetch(pageURL, resp.StatusCode, retryAfter)
	case http.StatusOK:
		return nil
	default:
		return &httpStatusError{code: resp.StatusCode, retryAfter: retryAfter}
	}
}

func (c *Client) handleRateLimitedFetch(pageURL string, statusCode int, retryAfter time.Duration) error {
	c.backoffState.RecordErrorWithSuggestedCooldown(retryAfter)
	cooldown := c.backoffState.HardCooldownRemaining()
	slog.Warn("YouTube rate limit hit, entering cooldown",
		"url", pageURL,
		"cooldown", cooldown.Round(time.Second),
		"retry_after", retryAfter.Round(time.Second))
	return &httpStatusError{code: statusCode, retryAfter: retryAfter, cause: ErrRateLimited}
}

func (c *Client) handleForbiddenFetch(pageURL string, statusCode int, retryAfter time.Duration) error {
	c.backoffState.RecordErrorWithSuggestedCooldown(retryAfter)
	slog.Warn("YouTube access forbidden",
		"url", pageURL,
		"retry_after", retryAfter.Round(time.Second))
	return &httpStatusError{code: statusCode, retryAfter: retryAfter, cause: ErrForbidden}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	retryAt, err := http.ParseTime(value)
	if err != nil {
		return 0
	}

	delay := retryAt.Sub(now)
	if delay <= 0 {
		return 0
	}
	return delay
}

func drainResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_, _ = io.CopyN(io.Discard, resp.Body, 4*1024)
}

func applyScraperHeaders(req *http.Request, snap ua.HeaderSnapshot) {
	req.Header.Set("User-Agent", snap.UserAgent)
	if snap.SecChUA != "" {
		req.Header.Set("Sec-CH-UA", snap.SecChUA)
		req.Header.Set("Sec-CH-UA-Mobile", "?0")
		req.Header.Set("Sec-CH-UA-Platform", snap.SecChUAPlatform)
	}

	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Accept", snap.Accept)
	req.Header.Set("Cookie", "SOCS=CAI")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cache-Control", "max-age=0")
}
