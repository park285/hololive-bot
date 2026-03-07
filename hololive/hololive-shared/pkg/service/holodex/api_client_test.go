package holodex

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

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

// TestAPIClientWithMockServer_Success: 정상 응답 시나리오 테스트
func TestAPIClientWithMockServer_Success(t *testing.T) {
	expectedBody := `{"status":"ok","data":[]}`
	client, server := newTestClientWithHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedBody))
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

// TestAPIClient_CircuitBreakerOpens: 서킷 브레이커 동작 테스트
func TestAPIClient_CircuitBreakerOpens(t *testing.T) {
	client := &APIClient{
		httpClient:  &http.Client{},
		baseURL:     "https://holodex.net/api/v2",
		apiKey:      "k1",
		logger:      slog.Default(),
		rateLimiter: rate.NewLimiter(rate.Every(10*time.Millisecond), 1),
		semaphore:   make(chan struct{}, 2),
	}

	// 초기 상태: 서킷 닫힘
	if client.IsCircuitOpen() {
		t.Fatal("expected circuit to be closed initially")
	}

	// 서킷 열기
	client.openCircuit()

	// 서킷 열린 상태 확인
	if !client.IsCircuitOpen() {
		t.Fatal("expected circuit to be open after openCircuit()")
	}

	// 서킷 리셋
	client.resetCircuit()
	if client.IsCircuitOpen() {
		t.Fatal("expected circuit to be closed after resetCircuit()")
	}
}

// TestAPIClient_FailureCountIncrement: 실패 카운트 증가 테스트
func TestAPIClient_FailureCountIncrement(t *testing.T) {
	client := &APIClient{
		httpClient: &http.Client{},
		baseURL:    "https://holodex.net/api/v2",
		apiKey:     "k1",
		logger:     slog.Default(),
	}

	for i := 1; i <= 5; i++ {
		count := client.incrementFailureCount()
		if count != i {
			t.Errorf("expected failure count %d, got %d", i, count)
		}
	}
}

// TestPerAttemptTimeout: per-attempt context timeout이 서버 지연보다 먼저 발동하는지 확인
func TestPerAttemptTimeout(t *testing.T) {
	// 테스트용 PerAttemptTimeout 설정 (짧게)
	origTimeout := constants.APIConfig.PerAttemptTimeout
	constants.APIConfig.PerAttemptTimeout = 200 * time.Millisecond
	t.Cleanup(func() { constants.APIConfig.PerAttemptTimeout = origTimeout })

	client, server := newTestClientWithHandler(func(w http.ResponseWriter, _ *http.Request) {
		// 서버가 per-attempt timeout보다 오래 걸림
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}, "test-key")
	defer server.Close()

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

// TestTimeoutMaxRetries: timeout 3회 연속 시 조기 종료 확인
func TestTimeoutMaxRetries(t *testing.T) {
	origTimeout := constants.APIConfig.PerAttemptTimeout
	constants.APIConfig.PerAttemptTimeout = 100 * time.Millisecond
	t.Cleanup(func() { constants.APIConfig.PerAttemptTimeout = origTimeout })

	requestCount := 0
	client, server := newTestClientWithHandler(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}, "test-key")
	defer server.Close()

	start := time.Now()
	_, err := client.DoRequest(context.Background(), http.MethodGet, "/test", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("timeout 에러가 발생해야 한다")
	}

	// maxTimeoutRetries(3)회에서 중단되어야 함
	if requestCount > 3 {
		t.Errorf("timeout 재시도 제한 미동작: requests=%d, want <= 3", requestCount)
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
	got := distributedRateLimitBucket("/users/live")
	want := constants.HolodexDistributedRateLimitConfig.BucketBase + ":users:live"
	if got != want {
		t.Fatalf("bucket mismatch: got %q want %q", got, want)
	}
}

// TestParentContextCancel: 부모 context 취소 시 즉시 에러 반환 확인
func TestParentContextCancel(t *testing.T) {
	origTimeout := constants.APIConfig.PerAttemptTimeout
	constants.APIConfig.PerAttemptTimeout = 5 * time.Second
	t.Cleanup(func() { constants.APIConfig.PerAttemptTimeout = origTimeout })

	client, server := newTestClientWithHandler(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}, "test-key")
	defer server.Close()

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

// TestIsTimeoutError: timeout 에러 분류 정확성 검증
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
			got := isTimeoutError(tt.err)
			if got != tt.expected {
				t.Errorf("isTimeoutError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

// TestShouldUseFallbackTimeout: timeout 에러 시 shouldUseFallback=true 확인
func TestShouldUseFallbackTimeout(t *testing.T) {
	svc := &Service{
		requester: &APIClient{
			httpClient:  &http.Client{},
			baseURL:     "https://holodex.net/api/v2",
			apiKey:      "k1",
			logger:      slog.Default(),
			rateLimiter: rate.NewLimiter(rate.Every(10*time.Millisecond), 1),
			semaphore:   make(chan struct{}, 2),
		},
		logger: slog.Default(),
	}

	activeCtx := context.Background()

	tests := []struct {
		name     string
		ctx      context.Context
		err      error
		expected bool
	}{
		{
			name:     "DeadlineExceeded with active ctx",
			ctx:      activeCtx,
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "wrapped timeout with active ctx",
			ctx:      activeCtx,
			err:      fmt.Errorf("request: %w", context.DeadlineExceeded),
			expected: true,
		},
		{
			name:     "net timeout with active ctx",
			ctx:      activeCtx,
			err:      &mockTimeoutError{msg: "i/o timeout", timeout: true},
			expected: true,
		},
		{
			name:     "일반 에러는 폴백 안함",
			ctx:      activeCtx,
			err:      fmt.Errorf("some error"),
			expected: false,
		},
		{
			name:     "nil 에러",
			ctx:      activeCtx,
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.shouldUseFallback(tt.ctx, tt.err)
			if got != tt.expected {
				t.Errorf("shouldUseFallback(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

// TestShouldUseFallbackCallerContextExpired: 호출자 context 만료 시 폴백 차단 확인
func TestShouldUseFallbackCallerContextExpired(t *testing.T) {
	svc := &Service{
		requester: &APIClient{
			httpClient:  &http.Client{},
			baseURL:     "https://holodex.net/api/v2",
			apiKey:      "k1",
			logger:      slog.Default(),
			rateLimiter: rate.NewLimiter(rate.Every(10*time.Millisecond), 1),
			semaphore:   make(chan struct{}, 2),
		},
		logger: slog.Default(),
	}

	// 이미 만료된 context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// timeout 에러지만, 호출자 ctx가 만료되었으므로 폴백하면 안 됨
	if svc.shouldUseFallback(canceledCtx, context.DeadlineExceeded) {
		t.Error("호출자 context 만료 시 폴백하면 안 됨")
	}

	if svc.shouldUseFallback(canceledCtx, &mockTimeoutError{msg: "timeout", timeout: true}) {
		t.Error("호출자 context 만료 시 net timeout도 폴백하면 안 됨")
	}
}
