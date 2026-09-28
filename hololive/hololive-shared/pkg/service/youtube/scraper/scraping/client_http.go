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

package scraping

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/service/youtube/scraper/ua"
)

// currentPageFetcher는 net/http fetcher 하나만 돌려준다. 차단 시그니처 본문을 browser snapshot으로 다시 가져오던
// 경로와 SCRAPER_FETCHER_ENGINE 선택은 production 호출자 없이 남아 있어 지웠다(stack-audit 2026-09-26 T11 C2).
// 차단·동의 페이지 응답은 대체 fetch 없이 그대로 오류로 드러난다.
func (c *Client) currentPageFetcher() pageFetcher {
	return netHTTPPageFetcher{client: c}
}

func (c *Client) fetchPageOnce(ctx context.Context, pageURL string) (body string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	snap := c.uaProvider.Headers(ctx)
	applyScraperHeaders(req, snap)

	started := time.Now()
	statusCode := 0

	defer func() {
		observeScraperFetch(statusCode, err, time.Since(started))
	}()

	resp, err := c.currentPageFetcher().FetchPage(ctx, pageFetchRequest{URL: pageURL, Header: req.Header})
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}

	statusCode = resp.StatusCode

	if statusErr := c.handleFetchStatus(pageURL, resp); statusErr != nil {
		return "", fmt.Errorf("handle fetch status: %w", statusErr)
	}

	if err := validateSuccessfulFetchBody(pageURL, resp.FinalURL, resp.Body); err != nil {
		return "", fmt.Errorf("validate successful fetch body: %w", err)
	}

	c.backoffState.RecordSuccess()

	return string(resp.Body), nil
}

func (c *Client) fetchPagePreflight(ctx context.Context, pageURL string) error {
	if cooldownRemaining := c.backoffState.HardCooldownRemaining(); cooldownRemaining > 0 {
		return fmt.Errorf("in cooldown for %v: %w", cooldownRemaining.Round(time.Second), ErrRateLimited)
	}

	bucket := distributedBucketFromURL(c.config.DistributedRateLimit.BucketBase, pageURL)

	decision, err := c.rateLimiter.TryReserveWithBucket(ctx, bucket)
	if err != nil {
		return fmt.Errorf("rate limiter admission failed: %w", err)
	}

	if !decision.Allowed {
		return newRateLimitAdmissionDeferredError(bucket, decision)
	}

	return nil
}

func (c *Client) handleFetchStatus(pageURL string, resp pageFetchResponse) error {
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		c.recordRateLimitedFetch(pageURL, retryAfter)

		return &httpStatusError{code: resp.StatusCode, retryAfter: retryAfter, cause: ErrRateLimited}
	case http.StatusForbidden:
		c.recordForbiddenFetch(pageURL, retryAfter)

		return &httpStatusError{code: resp.StatusCode, retryAfter: retryAfter, cause: ErrForbidden}
	case http.StatusOK:
		return nil
	default:
		return &httpStatusError{code: resp.StatusCode, retryAfter: retryAfter}
	}
}

func (c *Client) recordRateLimitedFetch(pageURL string, retryAfter time.Duration) {
	c.backoffState.RecordErrorWithSuggestedCooldown(retryAfter)

	cooldown := c.backoffState.HardCooldownRemaining()
	slog.Warn("YouTube rate limit hit, entering cooldown",
		"url", pageURL,
		"cooldown", cooldown.Round(time.Second),
		"retry_after", retryAfter.Round(time.Second))
}

func (c *Client) recordForbiddenFetch(pageURL string, retryAfter time.Duration) {
	c.backoffState.RecordErrorWithSuggestedCooldown(retryAfter)
	slog.Warn("YouTube access forbidden",
		"url", pageURL,
		"retry_after", retryAfter.Round(time.Second))
}

// MaxRetryAfterDuration: Retry-After 헤더가 비정상적으로 큰 값을 보낼 때 적용되는 상한.
// Channel-health/cooldown 계층에서 11일짜리 차단이 propagate되는 사고를 방지한다.
const MaxRetryAfterDuration = 6 * time.Hour

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}

		return clampRetryAfter(time.Duration(seconds) * time.Second)
	}

	retryAt, err := http.ParseTime(value)
	if err != nil {
		return 0
	}

	delay := retryAt.Sub(now)
	if delay <= 0 {
		return 0
	}

	return clampRetryAfter(delay)
}

func clampRetryAfter(delay time.Duration) time.Duration {
	if delay > MaxRetryAfterDuration {
		return MaxRetryAfterDuration
	}

	return delay
}

func drainResponseBody(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}

	if _, err := io.CopyN(io.Discard, resp.Body, 4*1024); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("drain response body: %w", err)
	}

	return nil
}

const successfulBodySignatureScanLimit = 64 * 1024

func validateSuccessfulFetchBody(pageURL, finalURL string, body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("%w: %s", ErrEmptyResponse, pageURL)
	}

	if finalURLLooksBlocked(finalURL) {
		return fmt.Errorf("%w: %s -> %s", ErrBlockedResponse, pageURL, finalURL)
	}

	if bodyLooksBlockedByYouTube(body) {
		slog.Warn("YouTube block-page signature found in fetch body without a blocked final URL; rejecting parser input without global cooldown",
			"url", pageURL,
			"final_url", finalURL)

		return fmt.Errorf("%w: %s -> %s", ErrBlockedBodySignature, pageURL, finalURL)
	}

	return nil
}

func finalURLLooksBlocked(finalURL string) bool {
	finalURL = strings.TrimSpace(finalURL)
	if finalURL == "" {
		return false
	}

	parsed, err := url.Parse(finalURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	path := strings.ToLower(parsed.Path)

	for _, marker := range blockedRedirectHosts {
		if host == marker.host && (marker.pathPrefix == "" || strings.HasPrefix(path, marker.pathPrefix)) {
			return true
		}
	}

	return false
}

var blockedRedirectHosts = []struct {
	host       string
	pathPrefix string
}{
	{host: "www.google.com", pathPrefix: "/sorry"},
	{host: "google.com", pathPrefix: "/sorry"},
	{host: "consent.youtube.com"},
	{host: "consent.google.com"},
	{host: "www.youtube.com", pathPrefix: "/sorry"},
}

func bodyLooksBlockedByYouTube(body []byte) bool {
	sample := body
	if len(sample) > successfulBodySignatureScanLimit {
		sample = sample[:successfulBodySignatureScanLimit]
	}

	lower := strings.ToLower(string(sample))

	for _, signature := range blockedResponseSignatures {
		if strings.Contains(lower, signature) {
			return true
		}
	}

	return false
}

var blockedResponseSignatures = []string{
	"youtube.com/sorry",
	"/sorry/index",
	"consent.youtube.com",
	"google.com/recaptcha",
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
