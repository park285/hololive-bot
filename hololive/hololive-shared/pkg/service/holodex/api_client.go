// Holodex API Integration
//
// 본 코드는 Holodex API (https://holodex.net)를 사용하며, Holodex API Terms of Service를 준수합니다.
//
// Attribution (Holodex API Terms Section 6):
//   - API Provider: Holodex (https://holodex.net)
//   - License: https://holodex.net/api/terms
//   - Disclaimer: THE HOLODEX API IS PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND.
//
// See: https://holodex.net/api/terms for full terms.

package holodex

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"
	"golang.org/x/time/rate"

	"github.com/kapu/hololive-shared/internal/ctxutil"
	"github.com/kapu/hololive-shared/internal/retry"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/util"
)

// Requester: HTTP 요청 수행 및 서킷 브레이커 상태 확인을 위한 인터페이스
type Requester interface {
	DoRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error)
	IsCircuitOpen() bool
}

// APIClient: Holodex API 요청을 처리하는 클라이언트
// 서킷 브레이커, 속도 제한(Rate Limiting) 기능을 포함합니다.
type APIClient struct {
	httpClient       *http.Client
	baseURL          string
	apiKey           string
	logger           *slog.Logger
	failureCount     int
	circuitOpenUntil *time.Time
	circuitMu        sync.RWMutex
	rateLimiter      *rate.Limiter // Rate limiter: 초당 10 요청
	semaphore        chan struct{} // Semaphore: 동시 API 요청 수 제한
	distributed      distributedRateLimiter
}

type distributedRateLimiter interface {
	Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimit.Decision, error)
}

var errNoAPIKeys = stdErrors.New("no Holodex API keys configured")

// NewHolodexAPIClient: 새로운 Holodex API 클라이언트를 생성하고 초기화합니다.
// 단일 API 키 사용, 초당 10회 요청 제한(Rate Limit)이 기본 설정된다.
// Semaphore로 동시 요청 수를 제한하여 API 과부하를 방지한다.
func NewHolodexAPIClient(
	httpClient *http.Client,
	baseURL string,
	apiKey string,
	logger *slog.Logger,
	distributed distributedRateLimiter,
) *APIClient {
	if httpClient == nil {
		httpClient = httputil.NewClient(constants.APIConfig.HolodexTimeout)
	}
	return &APIClient{
		httpClient:  httpClient,
		baseURL:     baseURL,
		apiKey:      apiKey,
		logger:      logger,
		rateLimiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 1), // 초당 10 요청
		semaphore:   make(chan struct{}, constants.HolodexConcurrencyConfig.MaxConcurrentRequests),
		distributed: distributed,
	}
}

// DoRequest: Holodex API에 요청을 보낸다.
// Rate Limit 준수, 서킷 브레이커 확인, API 키 로테이션 및 재시도 로직을 수행합니다.
// 매 시도(재시도 포함)마다 Rate Limiter를 통과하여 burst 요청을 방지합니다.
func (c *APIClient) DoRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	if err := c.rejectIfCircuitOpen(); err != nil {
		return nil, err
	}

	if c.apiKey == "" {
		return nil, errNoAPIKeys
	}

	maxAttempts := util.Min(1+constants.RetryConfig.MaxAttempts, 10)
	const maxTimeoutRetries = 3
	timeoutCount := 0
	var lastErr error

	for attempt := range maxAttempts {
		if err := c.waitForRateLimiter(ctx, path); err != nil {
			return nil, err
		}

		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context canceled before request: %w", err)
		}

		// 동시성 제한은 실제 HTTP 시도 구간에만 적용하고, backoff 구간에서는 즉시 반납한다.
		if err := c.acquireSemaphore(ctx); err != nil {
			return nil, err
		}

		body, done, err := c.tryHolodexRequest(ctx, method, path, params, attempt, maxAttempts)
		c.releaseSemaphore()
		if done {
			if err != nil {
				return nil, err
			}
			c.resetCircuit()
			return body, nil
		}

		if err != nil {
			lastErr = err
			if isTimeoutError(err) {
				timeoutCount++
				if timeoutCount >= maxTimeoutRetries {
					c.logger.Warn("Timeout retry limit reached",
						slog.Int("timeout_count", timeoutCount),
						slog.String("path", path),
					)
					break
				}
			}
		}

		if attempt < maxAttempts-1 {
			if err := c.waitBackoff(ctx, attempt); err != nil {
				return nil, err
			}
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, fmt.Errorf("holodex request failed after %d attempts", maxAttempts)
}

// waitForRateLimiter: Rate Limiter를 통과할 때까지 대기합니다.
func (c *APIClient) waitForRateLimiter(ctx context.Context, path string) error {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter wait failed: %w", err)
	}
	return c.waitForDistributedRateLimiter(ctx, path)
}

func (c *APIClient) waitForDistributedRateLimiter(ctx context.Context, path string) error {
	if c.distributed == nil || !constants.HolodexDistributedRateLimitConfig.Enabled {
		return nil
	}

	bucket := distributedRateLimitBucket(path)
	for {
		decision, err := c.distributed.Allow(
			ctx,
			bucket,
			constants.HolodexDistributedRateLimitConfig.Limit,
			constants.HolodexDistributedRateLimitConfig.Window,
		)
		if err != nil {
			return fmt.Errorf("distributed rate limiter allow failed: %w", err)
		}

		if decision.Allowed {
			return nil
		}
		if decision.RetryAfter <= 0 {
			return fmt.Errorf(
				"distributed rate limiter denied without retry_after: bucket=%s current=%d limit=%d",
				bucket,
				decision.Current,
				decision.Limit,
			)
		}

		if !ctxutil.SleepWithContext(ctx, decision.RetryAfter) {
			return fmt.Errorf("distributed rate limiter wait canceled: %w", ctx.Err())
		}
	}
}

func distributedRateLimitBucket(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		trimmed = "root"
	}
	normalized := strings.ReplaceAll(trimmed, "/", ":")
	return constants.HolodexDistributedRateLimitConfig.BucketBase + ":" + normalized
}

// waitBackoff: 재시도 전 exponential backoff + jitter 대기를 수행합니다.
func (c *APIClient) waitBackoff(ctx context.Context, attempt int) error {
	delay := retry.ComputeBackoffDelay(attempt, constants.RetryConfig.BaseDelay, constants.RetryConfig.Jitter)
	if !ctxutil.SleepWithContext(ctx, delay) {
		return fmt.Errorf("context canceled during backoff: %w", ctx.Err())
	}
	return nil
}

func (c *APIClient) rejectIfCircuitOpen() error {
	if !c.IsCircuitOpen() {
		return nil
	}

	c.circuitMu.RLock()
	var remainingMs int64
	if c.circuitOpenUntil != nil {
		remainingMs = time.Until(*c.circuitOpenUntil).Milliseconds()
	}
	c.circuitMu.RUnlock()

	c.logger.Warn("Circuit breaker is open", slog.Int64("retry_after_ms", remainingMs))
	return NewAPIError("Circuit breaker open", 503, map[string]any{
		"retry_after_ms": remainingMs,
	})
}

func (c *APIClient) tryHolodexRequest(ctx context.Context, method, path string, params url.Values, attempt, maxAttempts int) ([]byte, bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, constants.APIConfig.PerAttemptTimeout)
	defer cancel()

	reqURL := c.buildRequestURL(path, params)
	req, err := c.newRequest(attemptCtx, method, reqURL, c.getNextAPIKey())
	if err != nil {
		return nil, true, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.retryAfterNetworkFailure(ctx, err, attempt, maxAttempts) {
			return nil, false, fmt.Errorf("HTTP request failed (retrying): %w", err)
		}
		return nil, true, fmt.Errorf("HTTP request failed: %w", err)
	}

	body, readErr := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, false, fmt.Errorf("failed to read response: %w", readErr)
	}

	return c.processHolodexResponse(ctx, resp.StatusCode, body, reqURL, attempt, maxAttempts)
}

func (c *APIClient) buildRequestURL(path string, params url.Values) string {
	reqURL := c.baseURL + path
	if params != nil {
		reqURL += "?" + params.Encode()
	}
	return reqURL
}

func (c *APIClient) newRequest(ctx context.Context, method, reqURL string, apiKey string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-APIKEY", apiKey)
	// Holodex API Terms 준수를 위해 정직한 User-Agent 사용 (Section 6: Attribution)
	req.Header.Set("User-Agent", "api.capu.blog/hololive-bot (Linux; +https://api.capu.blog; Holodex API client)")
	return req, nil
}

func (c *APIClient) retryAfterNetworkFailure(ctx context.Context, err error, attempt, maxAttempts int) bool {
	// 부모 ctx 취소 시 불필요한 재시도 방지
	if ctx.Err() != nil {
		return false
	}

	errorType := "network"
	if isTimeoutError(err) {
		errorType = "timeout"
	}

	count := c.incrementFailureCount()
	if count >= constants.CircuitBreakerConfig.FailureThreshold {
		c.openCircuit()
		return false
	}

	if attempt < maxAttempts-1 {
		c.logger.Warn("Request failed, retrying",
			slog.Any("error", err),
			slog.String("error_type", errorType),
			slog.Int("attempt", attempt+1),
		)
		return true
	}

	return false
}

// isTimeoutError: timeout 계열 에러인지 판별
func isTimeoutError(err error) bool {
	if stdErrors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if stdErrors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

func (c *APIClient) processHolodexResponse(ctx context.Context, status int, body []byte, reqURL string, attempt, maxAttempts int) ([]byte, bool, error) {
	switch {
	case status == http.StatusTooManyRequests:
		c.logger.Warn("Holodex rate limited, retrying",
			slog.Int("status", status),
			slog.Int("attempt", attempt+1),
			slog.String("url", reqURL),
		)
		if attempt < maxAttempts-1 {
			return nil, false, nil
		}
		return nil, true, NewKeyRotationError("Holodex rate limit exhausted", status, map[string]any{
			"url": reqURL,
		})
	// Holodex 403은 권한/키 상태 문제일 가능성이 높아 retryable rate limit로 취급하지 않는다.
	case status == http.StatusForbidden:
		c.logger.Error("Holodex forbidden response",
			slog.Int("status", status),
			slog.Int("attempt", attempt+1),
			slog.String("url", reqURL),
			slog.String("body_preview", summarizeHolodexErrorBody(body)),
		)
		return nil, true, NewAPIError("Holodex forbidden", status, map[string]any{
			"operation": reqURL,
		})
	case status >= 500:
		return c.handleServerError(ctx, status, attempt, maxAttempts)
	case status >= 400:
		return nil, true, NewAPIError(fmt.Sprintf("Client error: %d", status), status, map[string]any{
			"operation": reqURL,
		})
	default:
		return body, true, nil
	}
}

func (c *APIClient) handleServerError(_ context.Context, status, attempt, maxAttempts int) ([]byte, bool, error) {
	count := c.incrementFailureCount()
	c.logger.Warn("Server error",
		slog.Int("status", status),
		slog.Int("failure_count", count),
	)

	if count >= constants.CircuitBreakerConfig.FailureThreshold {
		c.openCircuit()
		return nil, true, NewAPIError(fmt.Sprintf("Server error: %d", status), status, nil)
	}

	if attempt < maxAttempts-1 {
		return nil, false, NewAPIError(fmt.Sprintf("Server error: %d", status), status, nil)
	}

	return nil, true, NewAPIError(fmt.Sprintf("Server error: %d", status), status, nil)
}

// IsCircuitOpen: 현재 서킷 브레이커가 열려있는지(요청 차단 상태인지) 확인합니다.
func (c *APIClient) IsCircuitOpen() bool {
	c.circuitMu.RLock()
	defer c.circuitMu.RUnlock()

	if c.circuitOpenUntil == nil {
		return false
	}

	if time.Now().After(*c.circuitOpenUntil) {
		return false
	}

	return true
}

func (c *APIClient) getNextAPIKey() string {
	return c.apiKey
}

func (c *APIClient) openCircuit() {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	resetTime := time.Now().Add(constants.CircuitBreakerConfig.ResetTimeout)
	c.circuitOpenUntil = &resetTime
	c.failureCount = 0

	c.logger.Error("Holodex circuit breaker opened",
		slog.Duration("reset_timeout", constants.CircuitBreakerConfig.ResetTimeout),
	)
}

func (c *APIClient) resetCircuit() {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	c.failureCount = 0
	c.circuitOpenUntil = nil
}

func (c *APIClient) incrementFailureCount() int {
	c.circuitMu.Lock()
	defer c.circuitMu.Unlock()

	c.failureCount++
	return c.failureCount
}

func (c *APIClient) acquireSemaphore(ctx context.Context) error {
	select {
	case c.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("semaphore acquisition canceled: %w", ctx.Err())
	}
}

func (c *APIClient) releaseSemaphore() {
	select {
	case <-c.semaphore:
	default:
	}
}

func summarizeHolodexErrorBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	const maxPreviewLen = 256
	if len(trimmed) <= maxPreviewLen {
		return trimmed
	}
	return trimmed[:maxPreviewLen] + "..."
}
