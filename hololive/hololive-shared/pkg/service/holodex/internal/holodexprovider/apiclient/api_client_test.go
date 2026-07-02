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

package apiclient

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
	"golang.org/x/time/rate"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/util"
)

func writeAPIResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write api response: %v", err)
	}
}

func TestNewHolodexAPIClient_UsesExternalAPITransportProfileByDefault(t *testing.T) {
	t.Parallel()

	holodexCfg := config.DefaultHolodexOperationalConfig()
	client := NewHolodexAPIClient(nil, "https://holodex.net/api/v2", "test-key", slog.Default(), nil, &holodexCfg)
	if client == nil {
		t.Fatal("NewHolodexAPIClient() returned nil")
	}

	expected := httputil.NewExternalAPIClient(holodexCfg.Timeout)
	if client.httpClient.Timeout != expected.Timeout {
		t.Fatalf("Timeout = %s, want %s", client.httpClient.Timeout, expected.Timeout)
	}

	gotTransport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.httpClient.Transport)
	}
	wantTransport, ok := expected.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected transport type = %T, want *http.Transport", expected.Transport)
	}
	if gotTransport.MaxConnsPerHost != wantTransport.MaxConnsPerHost {
		t.Fatalf("MaxConnsPerHost = %d, want %d", gotTransport.MaxConnsPerHost, wantTransport.MaxConnsPerHost)
	}
	if gotTransport.MaxIdleConnsPerHost != wantTransport.MaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", gotTransport.MaxIdleConnsPerHost, wantTransport.MaxIdleConnsPerHost)
	}
	if gotTransport.ResponseHeaderTimeout != wantTransport.ResponseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", gotTransport.ResponseHeaderTimeout, wantTransport.ResponseHeaderTimeout)
	}
}

func TestHolodexAPIClientSingleKey(t *testing.T) {
	logger := slog.Default()
	client := &APIClient{
		httpClient: &http.Client{},
		baseURL:    "https://holodex.net/api/v2",
		apiKey:     "k1",
		logger:     logger,
	}

	for range 5 {
		got := client.getNextAPIKey()
		if got != "k1" {
			t.Fatalf("expected key 'k1', got '%s'", got)
		}
	}
}

func newHolodexTestClient(apiKey string) *APIClient {
	return &APIClient{
		httpClient: &http.Client{},
		baseURL:    "https://holodex.net/api/v2",
		apiKey:     apiKey,
		logger:     slog.Default(),
		breaker: util.NewBreaker(
			constants.CircuitBreakerConfig.FailureThreshold,
			constants.CircuitBreakerConfig.ResetTimeout,
		),
	}
}

func TestHolodexAPIClientDoRequestNoKeys(t *testing.T) {
	logger := slog.Default()
	// semaphore 초기화하여 deadlock 방지
	client := &APIClient{
		httpClient:  &http.Client{},
		baseURL:     "https://holodex.net/api/v2",
		apiKey:      "",
		logger:      logger,
		rateLimiter: rate.NewLimiter(rate.Every(10*time.Millisecond), 1),
		semaphore:   make(chan struct{}, 2),
	}

	_, err := client.DoRequest(context.Background(), http.MethodGet, "/live", nil)
	if err == nil {
		t.Fatalf("expected error when no API keys configured")
	}
	if !stdErrors.Is(err, errNoAPIKeys) {
		t.Fatalf("unexpected error: %v", err)
	}
}

type nilResponseTransport struct{}

func (nilResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestHolodexAPIClientDoRequestNilResponse(t *testing.T) {
	client := &APIClient{
		httpClient:        &http.Client{Transport: nilResponseTransport{}},
		baseURL:           "https://holodex.example/api/v2",
		apiKey:            "test-key",
		logger:            slog.Default(),
		rateLimiter:       rate.NewLimiter(rate.Inf, 1),
		semaphore:         make(chan struct{}, 5),
		perAttemptTimeout: time.Second,
		breaker: util.NewBreaker(
			constants.CircuitBreakerConfig.FailureThreshold,
			constants.CircuitBreakerConfig.ResetTimeout,
		),
	}

	_, err := client.DoRequest(context.Background(), http.MethodGet, "/live", nil)
	if err == nil {
		t.Fatal("expected error for nil HTTP response")
	}
	if got := err.Error(); !strings.Contains(got, "nil response") {
		t.Fatalf("error = %q, want nil response context", got)
	}
}

// newTestClient: Mock 서버 테스트용 APIClient 생성
// baseURL 오버라이드가 불가하므로, buildRequestURL을 우회하는 대신
// 실제 요청 URL을 인터셉트하는 RoundTripper를 사용
func newTestClientWithHandler(handler http.HandlerFunc, apiKey string) (*APIClient, *httptest.Server) {
	server := httptest.NewServer(handler)
	client := &APIClient{
		httpClient:  server.Client(),
		baseURL:     server.URL,
		apiKey:      apiKey,
		logger:      slog.Default(),
		rateLimiter: rate.NewLimiter(rate.Every(10*time.Millisecond), 1),
		semaphore:   make(chan struct{}, 5),
	}
	return client, server
}

func TestAPIClientWithMockServer_Success(t *testing.T) {
	expectedBody := `{"status":"ok","data":[]}`
	client, server := newTestClientWithHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		writeAPIResponse(t, w, expectedBody)
	}, "test-key-1")
	defer server.Close()

	// Mock 서버 URL로 요청 (constants.APIConfig.HolodexBaseURL 대신)
	// 실제로는 buildRequestURL을 통해 HolodexBaseURL을 사용하므로,
	// RoundTripper를 커스텀하거나 이 테스트는 단위 테스트 범위에서 제외
	// 여기서는 getNextAPIKey와 같은 내부 로직만 테스트
	key := client.getNextAPIKey()
	if key != "test-key-1" {
		t.Errorf("expected key 'test-key-1', got '%s'", key)
	}
}

func TestAPIClient_CircuitBreakerOpens(t *testing.T) {
	client := newHolodexTestClient("k1")

	// 초기 상태: 서킷 닫힘
	if client.IsCircuitOpen() {
		t.Fatal("expected circuit to be closed initially")
	}

	// threshold 횟수만큼 실패 → 서킷 열기
	for range constants.CircuitBreakerConfig.FailureThreshold {
		client.openCircuit()
	}

	// 서킷 열린 상태 확인
	if !client.IsCircuitOpen() {
		t.Fatal("expected circuit to be open after openCircuit()")
	}

	// openedAt을 과거로 설정하여 reset 트리거
	client.forceOpenedAtForTest(time.Now().Add(-constants.CircuitBreakerConfig.ResetTimeout - time.Second))

	if client.IsCircuitOpen() {
		t.Fatal("expected circuit to be closed after timeout")
	}
}

func TestAPIClient_FailureCountIncrement(t *testing.T) {
	client := newHolodexTestClient("k1")

	// threshold 횟수(3회) 미만에서는 circuit이 열리지 않아야 함
	for i := 1; i < constants.CircuitBreakerConfig.FailureThreshold; i++ {
		client.openCircuit()
		if client.IsCircuitOpen() {
			t.Errorf("circuit opened after %d failures (threshold=%d)", i, constants.CircuitBreakerConfig.FailureThreshold)
		}
	}

	// threshold 도달 시 circuit이 열려야 함
	client.openCircuit()
	if !client.IsCircuitOpen() {
		t.Errorf("circuit should be open after %d failures", constants.CircuitBreakerConfig.FailureThreshold)
	}
}

func TestHandleServerError_CircuitOpenStopsRetry(t *testing.T) {
	// M1: threshold 도달 시 handleServerError가 추가 재시도 없이 즉시 중단해야 합니다.
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &APIClient{
		httpClient:        server.Client(),
		baseURL:           server.URL,
		apiKey:            "test-key",
		logger:            slog.Default(),
		rateLimiter:       rate.NewLimiter(rate.Inf, 1),
		semaphore:         make(chan struct{}, 5),
		perAttemptTimeout: 5 * time.Second,
		breaker: util.NewBreaker(
			constants.CircuitBreakerConfig.FailureThreshold,
			constants.CircuitBreakerConfig.ResetTimeout,
		),
	}

	_, err := client.DoRequest(context.Background(), http.MethodGet, "/test", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	// threshold(3)회 요청 후 circuit이 열려 즉시 중단해야 합니다.
	// maxAttempts = 1 + RetryConfig.MaxAttempts(3) = 4이지만
	// threshold(3) 도달 시 4번째 시도 전에 중단해야 합니다.
	if requestCount != constants.CircuitBreakerConfig.FailureThreshold {
		t.Errorf("expected exactly %d requests (threshold), got %d",
			constants.CircuitBreakerConfig.FailureThreshold, requestCount)
	}

	if !client.IsCircuitOpen() {
		t.Error("circuit should be open after threshold failures")
	}
}

func TestHandleServerError_AfterResetRequiresThresholdAgain(t *testing.T) {
	// M3: timeout 경과 후 reset → 단 1회 실패로는 즉시 재open 없음
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &APIClient{
		httpClient:        server.Client(),
		baseURL:           server.URL,
		apiKey:            "test-key",
		logger:            slog.Default(),
		rateLimiter:       rate.NewLimiter(rate.Inf, 1),
		semaphore:         make(chan struct{}, 5),
		perAttemptTimeout: 5 * time.Second,
		breaker: util.NewBreaker(
			constants.CircuitBreakerConfig.FailureThreshold,
			constants.CircuitBreakerConfig.ResetTimeout,
		),
	}

	// threshold 도달 → circuit open
	for range constants.CircuitBreakerConfig.FailureThreshold {
		client.openCircuit()
	}
	if !client.IsCircuitOpen() {
		t.Fatal("should be open after threshold")
	}

	client.forceOpenedAtForTest(time.Now().Add(-constants.CircuitBreakerConfig.ResetTimeout - time.Second))
	if !client.breaker.Allow() {
		t.Fatal("should allow after timeout")
	}

	client.openCircuit()
	if client.IsCircuitOpen() {
		t.Fatalf("single failure after reset must not re-open (threshold=%d)", constants.CircuitBreakerConfig.FailureThreshold)
	}
}

func TestPerAttemptTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		writeAPIResponse(t, w, `{}`)
	}))
	defer server.Close()

	holodexCfg := config.DefaultHolodexOperationalConfig()
	holodexCfg.PerAttemptTimeout = 200 * time.Millisecond

	client := &APIClient{
		httpClient:        server.Client(),
		baseURL:           server.URL,
		apiKey:            "test-key",
		logger:            slog.Default(),
		rateLimiter:       rate.NewLimiter(rate.Every(10*time.Millisecond), 1),
		semaphore:         make(chan struct{}, 5),
		perAttemptTimeout: holodexCfg.PerAttemptTimeout,
	}

	start := time.Now()
	_, err := client.DoRequest(context.Background(), http.MethodGet, "/test", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("timeout 에러가 발생해야 한다")
	}

	// 각 시도가 200ms timeout이므로, 전체 소요 시간이 http.Client.Timeout(기본 0)보다 훨씬 짧아야 한다
	// 3회 timeout + backoff = ~1s 이내
	if elapsed > 5*time.Second {
		t.Errorf("per-attempt timeout이 동작하지 않음: elapsed=%v", elapsed)
	}
}

func TestTimeoutMaxRetries(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &APIClient{
		httpClient:        server.Client(),
		baseURL:           server.URL,
		apiKey:            "test-key",
		logger:            slog.Default(),
		rateLimiter:       rate.NewLimiter(rate.Every(10*time.Millisecond), 1),
		semaphore:         make(chan struct{}, 5),
		perAttemptTimeout: 100 * time.Millisecond,
	}

	start := time.Now()
	_, err := client.DoRequest(context.Background(), http.MethodGet, "/test", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("timeout 에러가 발생해야 한다")
	}

	// maxTimeoutRetries(3)회에서 중단되어야 함
	if got := requestCount.Load(); got > 3 {
		t.Errorf("timeout 재시도 제한 미동작: requests=%d, want <= 3", got)
	}

	// 3회 × 100ms + backoff ≈ 2s 이내
	if elapsed > 5*time.Second {
		t.Errorf("조기 종료 미동작: elapsed=%v", elapsed)
	}
}

type stubDistributedLimiter struct {
	mu        sync.Mutex
	decisions []ratelimit.Decision
	calls     int
}

func (s *stubDistributedLimiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (ratelimit.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++

	if len(s.decisions) == 0 {
		return ratelimit.Decision{Allowed: true}, nil
	}
	if len(s.decisions) == 1 {
		return s.decisions[0], nil
	}

	d := s.decisions[0]
	s.decisions = s.decisions[1:]
	return d, nil
}

func TestWaitForRateLimiter_DistributedDeniedThenAllowed(t *testing.T) {
	client := &APIClient{
		rateLimiter: rate.NewLimiter(rate.Every(0), 1),
		distributed: &stubDistributedLimiter{
			decisions: []ratelimit.Decision{
				{Allowed: false, RetryAfter: 5 * time.Millisecond, Current: 10, Limit: 10},
				{Allowed: true, Current: 9, Limit: 10},
			},
		},
		distributedRLCfg: config.DistributedRateLimitConfig{
			Enabled:    true,
			BucketBase: "holodex:api",
		},
	}

	err := client.waitForRateLimiter(context.Background(), "/videos")
	if err != nil {
		t.Fatalf("waitForRateLimiter() error = %v", err)
	}
}

func TestWaitForRateLimiter_DistributedDeniedWithoutRetryAfter(t *testing.T) {
	client := &APIClient{
		rateLimiter: rate.NewLimiter(rate.Every(0), 1),
		distributed: &stubDistributedLimiter{
			decisions: []ratelimit.Decision{
				{Allowed: false, RetryAfter: 0, Current: 10, Limit: 10},
			},
		},
		distributedRLCfg: config.DistributedRateLimitConfig{
			Enabled:    true,
			BucketBase: "holodex:api",
		},
	}

	err := client.waitForRateLimiter(context.Background(), "/videos")
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
}

func TestProcessHolodexResponse_ForbiddenDoesNotRetry(t *testing.T) {
	client := &APIClient{
		logger: slog.Default(),
	}

	_, done, err := client.processHolodexResponse(
		context.Background(),
		http.StatusForbidden,
		[]byte(`{"error":"invalid api key"}`),
		"https://holodex.example/api/v2/live",
		0,
		4,
	)
	if !done {
		t.Fatal("expected 403 response to stop retry loop")
	}
	if err == nil {
		t.Fatal("expected 403 response to return error")
	}

	var apiErr *APIError
	if !stdErrors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", apiErr.StatusCode, http.StatusForbidden)
	}
}

func TestProcessHolodexResponse_RateLimitedRetriesBeforeExhaustion(t *testing.T) {
	client := &APIClient{
		logger: slog.Default(),
	}

	_, done, err := client.processHolodexResponse(
		context.Background(),
		http.StatusTooManyRequests,
		nil,
		"https://holodex.example/api/v2/live",
		0,
		4,
	)
	if done {
		t.Fatal("expected 429 response to keep retry loop running")
	}
	if err != nil {
		t.Fatalf("expected nil error before retries are exhausted, got %v", err)
	}
}

func TestProcessHolodexResponse_RateLimitedExhaustionReturnsKeyRotationError(t *testing.T) {
	client := &APIClient{
		logger: slog.Default(),
	}

	_, done, err := client.processHolodexResponse(
		context.Background(),
		http.StatusTooManyRequests,
		nil,
		"https://holodex.example/api/v2/live",
		3,
		4,
	)
	if !done {
		t.Fatal("expected final 429 response to stop retry loop")
	}
	if err == nil {
		t.Fatal("expected final 429 response to return error")
	}

	var rotationErr *KeyRotationError
	if !stdErrors.As(err, &rotationErr) {
		t.Fatalf("expected KeyRotationError, got %T", err)
	}
	if rotationErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rotationErr.StatusCode, http.StatusTooManyRequests)
	}
}

func TestDistributedRateLimitBucket(t *testing.T) {
	holodexCfg := config.DefaultHolodexOperationalConfig()
	client := &APIClient{
		distributedRLCfg: holodexCfg.DistributedRateLimit,
	}
	got := client.distributedRateLimitBucket("/users/live")
	want := holodexCfg.DistributedRateLimit.BucketBase + ":users:live"
	if got != want {
		t.Fatalf("bucket mismatch: got %q want %q", got, want)
	}
}

func TestParentContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &APIClient{
		httpClient:        server.Client(),
		baseURL:           server.URL,
		apiKey:            "test-key",
		logger:            slog.Default(),
		rateLimiter:       rate.NewLimiter(rate.Every(10*time.Millisecond), 1),
		semaphore:         make(chan struct{}, 5),
		perAttemptTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.DoRequest(ctx, http.MethodGet, "/test", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("context 취소로 에러가 발생해야 한다")
	}

	// 부모 ctx 200ms 이후 즉시 종료
	if elapsed > 2*time.Second {
		t.Errorf("부모 context 취소 후 즉시 종료되지 않음: elapsed=%v", elapsed)
	}
}

// mockTimeoutError: net.Error 인터페이스를 구현하는 mock timeout 에러
type mockTimeoutError struct {
	msg     string
	timeout bool
}

func (e *mockTimeoutError) Error() string   { return e.msg }
func (e *mockTimeoutError) Timeout() bool   { return e.timeout }
func (e *mockTimeoutError) Temporary() bool { return false }

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "context.DeadlineExceeded",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "wrapped DeadlineExceeded",
			err:      fmt.Errorf("request failed: %w", context.DeadlineExceeded),
			expected: true,
		},
		{
			name:     "net.Error with Timeout=true",
			err:      &mockTimeoutError{msg: "i/o timeout", timeout: true},
			expected: true,
		},
		{
			name: "wrapped net.Error with Timeout=true",
			err: fmt.Errorf("HTTP failed: %w",
				&net.OpError{Op: "dial", Err: &mockTimeoutError{msg: "timeout", timeout: true}}),
			expected: true,
		},
		{
			name:     "net.Error with Timeout=false",
			err:      &mockTimeoutError{msg: "connection refused", timeout: false},
			expected: false,
		},
		{
			name:     "일반 에러",
			err:      fmt.Errorf("some error"),
			expected: false,
		},
		{
			name:     "context.Canceled (timeout 아님)",
			err:      context.Canceled,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTimeoutError(tt.err)
			if got != tt.expected {
				t.Errorf("isTimeoutError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
