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
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/internal/retry"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

const FetchPageMaxAttempts = 3

type FetchPolicy struct {
	MaxAttempts int
}

var (
	DefaultFetchPolicy              = FetchPolicy{MaxAttempts: FetchPageMaxAttempts}
	HighFrequencyChannelFetchPolicy = FetchPolicy{MaxAttempts: 1}
	MetadataResolveFetchPolicy      = FetchPolicy{MaxAttempts: 1}
)

var ErrRateLimited = errors.New("rate limited by YouTube (429)")

var ErrForbidden = errors.New("forbidden by YouTube (403)")

var ErrChannelNotFound = errors.New("channel does not exist")

var ErrChannelUnavailable = errors.New("channel is unavailable")

// httpStatusError: HTTP 상태 코드 기반 에러 (재시도 판단용)
type httpStatusError struct {
	code       int
	retryAfter time.Duration
	cause      error
}

func (e *httpStatusError) Error() string {
	if e.retryAfter > 0 {
		return fmt.Sprintf("unexpected status code: %d (retry-after: %s)", e.code, e.retryAfter.Round(time.Second))
	}
	return fmt.Sprintf("unexpected status code: %d", e.code)
}

func (e *httpStatusError) Unwrap() error {
	return e.cause
}

func extractHTTPStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return 0, false
	}
	return statusErr.code, true
}

func extractHTTPRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return 0
	}
	return statusErr.retryAfter
}

func isRetryableStatusError(err error) bool {
	statusCode, ok := extractHTTPStatusCode(err)
	return ok && isRetryableStatusCode(statusCode)
}

func isRetryableVideoPageError(err error) bool {
	return isRetryableStatusError(err) || isRetryableTransportError(err)
}

func isRetryableStatusCode(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooEarly:
		return true
	default:
		return isRetryable5xx(code)
	}
}

// isRetryable5xx: 5xx 서버 에러인지 확인 (재시도 대상)
func isRetryable5xx(code int) bool {
	switch code {
	case 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// isRetryableTransportError: 네트워크/프록시 계층 일시 장애인지 확인
func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}

	// 호출자 컨텍스트 취소는 재시도하지 않는다.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// 호출자 deadline 초과는 재시도하지 않는다.
	// 단, http.Client 자체 타임아웃은 문자열 시그니처로 구분하여 재시도 허용.
	if errors.Is(err, context.DeadlineExceeded) {
		return hasTransientTransportSignature(err.Error())
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if isTimeoutOrTemporaryError(urlErr) {
			return true
		}
		if urlErr.Err == nil {
			return false
		}
		if isTimeoutOrTemporaryError(urlErr.Err) {
			return true
		}
		return hasTransientTransportSignature(urlErr.Err.Error())
	}

	if isTimeoutOrTemporaryError(err) {
		return true
	}

	return hasTransientTransportSignature(err.Error())
}

type temporaryError interface {
	Temporary() bool
}

func isTimeoutOrTemporaryError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var tempErr temporaryError
	return errors.As(err, &tempErr) && tempErr.Temporary()
}

func hasTransientTransportSignature(msg string) bool {
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

type Client struct {
	httpClient       *http.Client // 테스트/특수 경로용 고정 클라이언트
	directHTTPClient *http.Client
	proxyHTTPClient  *http.Client
	directTransport  *http.Transport
	proxyTransport   *http.Transport
	activeHTTPClient atomic.Pointer[http.Client]
	proxyEnabled     atomic.Bool
	uaProvider       ua.Provider
	rateLimiter      *RateLimiter
	backoffState     *BackoffState
	proxyConfig      ProxyConfig
	stateStore       stateStore
	fetcherEngine    FetcherEngine

	communityMissing *cacheState
	videoRSSBackoff  *cacheState
}

type ClientOption func(*Client)

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

func WithUAProvider(provider ua.Provider) ClientOption {
	return func(c *Client) {
		c.uaProvider = provider
	}
}

func WithRateLimiter(rl *RateLimiter) ClientOption {
	return func(c *Client) {
		c.rateLimiter = rl
	}
}

func WithStateStore(store stateStore) ClientOption {
	return func(c *Client) {
		c.stateStore = store
	}
}

func WithFetcherEngine(engine FetcherEngine) ClientOption {
	return func(c *Client) {
		c.fetcherEngine = normalizeFetcherEngine(engine)
	}
}

func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		uaProvider:    ua.NewRotatingProvider(ua.StrategySessionTTL, 45*time.Minute),
		rateLimiter:   NewRateLimiter(3 * time.Second),
		backoffState:  NewBackoffState(),
		fetcherEngine: FetcherEngineNetHTTP,
	}

	// 옵션 적용 (프록시 설정 포함)
	for _, opt := range opts {
		opt(c)
	}

	// stateStore 주입 후 cacheState 초기화
	c.initStateManagers()
	c.initHTTPClients()

	return c
}

func (c *Client) ResolveCommunityPostPublishedAt(ctx context.Context, postID string) (*time.Time, error) {
	trimmedPostID := strings.TrimSpace(postID)
	if trimmedPostID == "" {
		return nil, ErrCommunityPublishedAtNotFound
	}

	html, err := c.fetchPage(ctx, fmt.Sprintf("https://www.youtube.com/post/%s", trimmedPostID), MetadataResolveFetchPolicy)
	if err != nil {
		return nil, fmt.Errorf("fetch community post page: %w", err)
	}

	publishedAt, err := extractCommunityPublishedAtFromHTML(html)
	if err != nil {
		return nil, fmt.Errorf("extract community published_at: %w", err)
	}

	return publishedAt, nil
}

func (c *Client) ResolveVideoPublishedAt(ctx context.Context, videoID string) (*time.Time, error) {
	trimmedVideoID := strings.TrimSpace(videoID)
	if trimmedVideoID == "" {
		return nil, ErrPublishedAtNotFound
	}

	html, err := c.fetchPage(ctx, fmt.Sprintf("https://www.youtube.com/watch?v=%s", trimmedVideoID), MetadataResolveFetchPolicy)
	if err != nil {
		return nil, fmt.Errorf("fetch video page: %w", err)
	}

	publishedAt, err := extractPublishedAtFromHTML(html)
	if err != nil {
		return nil, fmt.Errorf("extract video published_at: %w", err)
	}

	return publishedAt, nil
}

// fetchPage: YouTube 페이지 HTML 가져오기 (5xx 에러 시 재시도 포함)
func (c *Client) fetchPage(ctx context.Context, pageURL string, policy ...FetchPolicy) (string, error) {
	// transient cooldown 대기 (호출 간 감속, 내부 재시도와 독립)
	if wait := c.backoffState.TransientCooldownRemaining(); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("transient cooldown wait canceled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	resolvedPolicy := DefaultFetchPolicy
	if len(policy) > 0 && policy[0].MaxAttempts > 0 {
		resolvedPolicy = policy[0]
	}

	var result string

	err := retry.WithRetry(ctx, retry.RetryOptions{
		MaxAttempts: resolvedPolicy.MaxAttempts,
		BaseDelay:   2 * time.Second,
		Jitter:      1500 * time.Millisecond,
		ShouldRetry: func(err error) bool {
			if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrForbidden) {
				return false
			}
			var statusErr *httpStatusError
			if errors.As(err, &statusErr) {
				return isRetryableStatusCode(statusErr.code)
			}
			return isRetryableTransportError(err)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			if isRetryableTransportError(err) {
				c.closeIdleConnections()
			}
			slog.Debug("Scraper retry",
				"url", pageURL,
				"attempt", attempt,
				"delay", delay.Round(time.Millisecond),
				"error", err)
		},
	}, func(ctx context.Context) error {
		var err error
		result, err = c.fetchPageOnce(ctx, pageURL)
		return err
	})

	if err != nil {
		// context 취소/타임아웃 시 transient 에러 기록 스킵 (셧다운 시 불필요한 cooldown 방지)
		// retry 모두 소진된 경우에만 transient 에러 기록 (내부 retry 교차 오염 방지)
		if statusCode, ok := extractHTTPStatusCode(err); ctx.Err() == nil && ok && isRetryableStatusCode(statusCode) {
			c.backoffState.RecordTransientErrorWithSuggestedCooldown(extractHTTPRetryAfter(err))
		}
		return "", fmt.Errorf("fetchPage failed after retries: %w", err)
	}
	return result, nil
}

func (c *Client) currentPageFetcher() pageFetcher {
	netHTTPFetcher := netHTTPPageFetcher{client: c}
	if normalizeFetcherEngine(c.fetcherEngine) == FetcherEngineGoScrapy {
		return goscrapyPageFetcher{client: c, fallback: netHTTPFetcher}
	}
	return netHTTPFetcher
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

	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		c.backoffState.RecordErrorWithSuggestedCooldown(retryAfter)
		cooldown := c.backoffState.HardCooldownRemaining()
		slog.Warn("YouTube rate limit hit, entering cooldown",
			"url", pageURL,
			"cooldown", cooldown.Round(time.Second),
			"retry_after", retryAfter.Round(time.Second))
		return "", fmt.Errorf("status %d: %w", resp.StatusCode, ErrRateLimited)

	case http.StatusForbidden:
		c.backoffState.RecordErrorWithSuggestedCooldown(retryAfter)
		slog.Warn("YouTube access forbidden",
			"url", pageURL,
			"retry_after", retryAfter.Round(time.Second))
		return "", fmt.Errorf("status %d: %w", resp.StatusCode, ErrForbidden)

	case http.StatusOK:
		// body 읽기 성공 후에 RecordSuccess 호출

	default:
		return "", &httpStatusError{code: resp.StatusCode, retryAfter: retryAfter}
	}

	c.backoffState.RecordSuccess()
	return string(resp.Body), nil
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
