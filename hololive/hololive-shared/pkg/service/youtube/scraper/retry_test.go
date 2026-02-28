package scraper

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/ua"
)

type fixedSnapshotProvider struct {
	snap ua.HeaderSnapshot
}

func (p *fixedSnapshotProvider) Headers(_ context.Context) ua.HeaderSnapshot {
	return p.snap
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type flakyNetError struct {
	msg     string
	timeout bool
}

func (e *flakyNetError) Error() string {
	return e.msg
}

func (e *flakyNetError) Timeout() bool {
	return e.timeout
}

func (e *flakyNetError) Temporary() bool {
	return true
}

type stubDialer struct {
	delay time.Duration
	conn  net.Conn
	err   error
}

func (d *stubDialer) Dial(_, _ string) (net.Conn, error) {
	if d.delay > 0 {
		time.Sleep(d.delay)
	}
	if d.err != nil {
		return nil, d.err
	}
	return d.conn, nil
}

type trackingConn struct {
	net.Conn
	closeCh   chan struct{}
	closeOnce sync.Once
}

func newTrackingConn(conn net.Conn) *trackingConn {
	return &trackingConn{
		Conn:    conn,
		closeCh: make(chan struct{}),
	}
}

func (c *trackingConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeCh)
	})
	return c.Conn.Close()
}

func TestFetchPage_Retry5xx(t *testing.T) {
	tests := []struct {
		name           string
		statusSequence []int
		expectSuccess  bool
		expectAttempts int
	}{
		{
			name:           "504 then 200 succeeds",
			statusSequence: []int{504, 200},
			expectSuccess:  true,
			expectAttempts: 2,
		},
		{
			name:           "502 502 200 succeeds on third attempt",
			statusSequence: []int{502, 502, 200},
			expectSuccess:  true,
			expectAttempts: 3,
		},
		{
			name:           "504 504 504 fails after max retries",
			statusSequence: []int{504, 504, 504},
			expectSuccess:  false,
			expectAttempts: 3,
		},
		{
			name:           "500 503 504 fails after max retries",
			statusSequence: []int{500, 503, 504},
			expectSuccess:  false,
			expectAttempts: 3,
		},
		{
			name:           "200 succeeds immediately",
			statusSequence: []int{200},
			expectSuccess:  true,
			expectAttempts: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				idx := int(attempts.Add(1)) - 1
				if idx >= len(tt.statusSequence) {
					idx = len(tt.statusSequence) - 1
				}
				status := tt.statusSequence[idx]
				if status == 200 {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
				} else {
					w.WriteHeader(status)
				}
			}))
			defer server.Close()

			client := NewClient(
				WithHTTPClient(server.Client()),
				WithRateLimiter(NewRateLimiter(0)),
				WithUAProvider(ua.NewStaticProvider("test-agent")),
			)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			result, err := client.fetchPage(ctx, server.URL)

			assert.Equal(t, int32(tt.expectAttempts), attempts.Load())

			if tt.expectSuccess {
				require.NoError(t, err)
				assert.Contains(t, result, "ytInitialData")
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unexpected status code")
			}
		})
	}
}

func TestFetchPage_NoRetryOn429(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.fetchPage(ctx, server.URL)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimited)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestFetchPage_NoRetryOn403(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.fetchPage(ctx, server.URL)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrForbidden)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestFetchPage_NoRetryOn4xx(t *testing.T) {
	clientErrors := []int{400, 401, 404, 405}

	for _, status := range clientErrors {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()

			client := NewClient(
				WithHTTPClient(server.Client()),
				WithRateLimiter(NewRateLimiter(0)),
				WithUAProvider(ua.NewStaticProvider("test-agent")),
			)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := client.fetchPage(ctx, server.URL)

			require.Error(t, err)
			assert.Equal(t, int32(1), attempts.Load())
		})
	}
}

func TestFetchPage_TransportRetryClassification(t *testing.T) {
	tests := []struct {
		name             string
		firstErr         func(req *http.Request) error
		expectAttempts   int32
		expectSuccess    bool
		expectErrIs      error
		expectErrContain string
	}{
		{
			name: "transient transport reset retried",
			firstErr: func(req *http.Request) error {
				return &url.Error{
					Op:  http.MethodGet,
					URL: req.URL.String(),
					Err: &flakyNetError{msg: "read: connection reset by peer"},
				}
			},
			expectAttempts: 2,
			expectSuccess:  true,
		},
		{
			name: "http2 response header timeout retried",
			firstErr: func(req *http.Request) error {
				return &url.Error{
					Op:  http.MethodGet,
					URL: req.URL.String(),
					Err: &flakyNetError{msg: "http2: timeout awaiting response headers", timeout: true},
				}
			},
			expectAttempts: 2,
			expectSuccess:  true,
		},
		{
			name: "client timeout signature retried",
			firstErr: func(req *http.Request) error {
				return &url.Error{
					Op:  http.MethodGet,
					URL: req.URL.String(),
					Err: &flakyNetError{
						msg:     "context deadline exceeded (Client.Timeout exceeded while awaiting headers)",
						timeout: true,
					},
				}
			},
			expectAttempts: 2,
			expectSuccess:  true,
		},
		{
			name: "caller deadline exceeded not retried",
			firstErr: func(*http.Request) error {
				return context.DeadlineExceeded
			},
			expectAttempts: 1,
			expectSuccess:  false,
			expectErrIs:    context.DeadlineExceeded,
		},
		{
			name: "non transient url error not retried",
			firstErr: func(req *http.Request) error {
				return &url.Error{
					Op:  http.MethodGet,
					URL: req.URL.String(),
					Err: errors.New("x509: certificate signed by unknown authority"),
				}
			},
			expectAttempts:   1,
			expectSuccess:    false,
			expectErrContain: "x509",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32

			httpClient := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if attempts.Add(1) == 1 {
						return nil, tt.firstErr(req)
					}

					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("<html>ytInitialData = {};</html>")),
					}, nil
				}),
				Timeout: 2 * time.Second,
			}

			client := NewClient(
				WithHTTPClient(httpClient),
				WithRateLimiter(NewRateLimiter(0)),
				WithUAProvider(ua.NewStaticProvider("test-agent")),
			)

			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()

			result, err := client.fetchPage(ctx, "https://example.com/channel/test")
			assert.Equal(t, tt.expectAttempts, attempts.Load())

			if tt.expectSuccess {
				require.NoError(t, err)
				assert.Contains(t, result, "ytInitialData")
				return
			}

			require.Error(t, err)
			if tt.expectErrIs != nil {
				assert.ErrorIs(t, err, tt.expectErrIs)
			}
			if tt.expectErrContain != "" {
				assert.Contains(t, err.Error(), tt.expectErrContain)
			}
		})
	}
}

func TestClient_SetProxyEnabled_NoProxyConfigured(t *testing.T) {
	client := NewClient(
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
		WithProxy(ProxyConfig{Enabled: false, URL: ""}),
	)

	applied := client.SetProxyEnabled(true)
	assert.False(t, applied)
	assert.False(t, client.ProxyEnabled())
}

func TestClient_SetProxyEnabled_CustomHTTPClient_NoOp(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("<html>ytInitialData = {};</html>")),
			}, nil
		}),
		Timeout: 2 * time.Second,
	}

	client := NewClient(
		WithHTTPClient(httpClient),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
		WithProxy(ProxyConfig{Enabled: true, URL: "socks5://127.0.0.1:1080"}),
	)

	applied := client.SetProxyEnabled(false)
	assert.False(t, applied)
	assert.False(t, client.ProxyEnabled())
}

func TestDialSOCKS5WithContextFallback_CancelClosesConn(t *testing.T) {
	clientSide, peerSide := net.Pipe()
	tracking := newTrackingConn(clientSide)
	t.Cleanup(func() {
		_ = tracking.Close()
		_ = peerSide.Close()
	})

	dialer := &stubDialer{
		delay: 50 * time.Millisecond,
		conn:  tracking,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	conn, err := dialSOCKS5WithContextFallback(ctx, dialer, "tcp", "example.com:443")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, conn)

	select {
	case <-tracking.closeCh:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected dial fallback to close connection on context cancel")
	}
}

func TestDialSOCKS5WithContextFallback_SuccessKeepsConnOpen(t *testing.T) {
	clientSide, peerSide := net.Pipe()
	tracking := newTrackingConn(clientSide)
	t.Cleanup(func() {
		_ = tracking.Close()
		_ = peerSide.Close()
	})

	dialer := &stubDialer{
		delay: 5 * time.Millisecond,
		conn:  tracking,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, err := dialSOCKS5WithContextFallback(ctx, dialer, "tcp", "example.com:443")
	require.NoError(t, err)
	require.NotNil(t, conn)

	select {
	case <-tracking.closeCh:
		t.Fatal("connection should remain open on successful dial")
	default:
	}
}

func TestIsRetryable5xx(t *testing.T) {
	tests := []struct {
		code     int
		expected bool
	}{
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{501, false},
		{505, false},
		{200, false},
		{400, false},
		{429, false},
		{403, false},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			assert.Equal(t, tt.expected, isRetryable5xx(tt.code))
		})
	}
}

func TestHttpStatusError(t *testing.T) {
	err := &httpStatusError{code: 504}
	assert.Equal(t, "unexpected status code: 504", err.Error())
}

// --- Phase 3 테스트: BackoffState ---

func TestBackoffState_DualState(t *testing.T) {
	bs := NewBackoffState()

	// hard 에러 기록
	bs.RecordError()
	hardCD := bs.HardCooldownRemaining()
	assert.Greater(t, hardCD, time.Duration(0), "hard cooldown should be active after RecordError")

	// transient 에러 기록
	bs.RecordTransientError()
	transientCD := bs.TransientCooldownRemaining()
	assert.Greater(t, transientCD, time.Duration(0), "transient cooldown should be active after RecordTransientError")

	// 두 카운터 독립 검증
	assert.Greater(t, hardCD, transientCD, "hard cooldown should be longer than transient")
}

func TestBackoffState_RecordTransientError(t *testing.T) {
	bs := NewBackoffState()

	// 1회: 30초
	bs.RecordTransientError()
	cd1 := bs.TransientCooldownRemaining()
	assert.InDelta(t, 30*time.Second, cd1, float64(2*time.Second))

	// 2회: 3분
	bs.RecordTransientError()
	cd2 := bs.TransientCooldownRemaining()
	assert.InDelta(t, 3*time.Minute, cd2, float64(5*time.Second))

	// 3회: 10분
	bs.RecordTransientError()
	cd3 := bs.TransientCooldownRemaining()
	assert.InDelta(t, 10*time.Minute, cd3, float64(5*time.Second))
}

func TestBackoffState_CounterDecayOnExpiry(t *testing.T) {
	bs := NewBackoffState()

	// 3회 누적 시 10분 쿨다운
	bs.RecordTransientError()
	bs.RecordTransientError()
	bs.RecordTransientError()

	cd := bs.TransientCooldownRemaining()
	assert.InDelta(t, 10*time.Minute, cd, float64(5*time.Second))

	// 만료 시뮬레이션
	bs.transientCooldown = time.Now().Add(-1 * time.Second)
	assert.Equal(t, time.Duration(0), bs.TransientCooldownRemaining())

	// 만료 후 첫 에러는 30초로 시작해야 함 (카운터 리셋 검증)
	bs.RecordTransientError()
	cdAfterDecay := bs.TransientCooldownRemaining()
	assert.InDelta(t, 30*time.Second, cdAfterDecay, float64(2*time.Second))
}

func TestBackoffState_SuccessResetsAll(t *testing.T) {
	bs := NewBackoffState()

	bs.RecordError()
	bs.RecordTransientError()
	assert.Greater(t, bs.HardCooldownRemaining(), time.Duration(0))
	assert.Greater(t, bs.TransientCooldownRemaining(), time.Duration(0))

	bs.RecordSuccess()
	assert.Equal(t, time.Duration(0), bs.HardCooldownRemaining())
	assert.Equal(t, time.Duration(0), bs.TransientCooldownRemaining())
	assert.Equal(t, time.Duration(0), bs.CooldownRemaining())
}

func TestBackoffState_ConcurrentAccess(t *testing.T) {
	bs := NewBackoffState()
	var wg sync.WaitGroup

	for range 10 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			bs.RecordError()
		}()
		go func() {
			defer wg.Done()
			bs.RecordTransientError()
		}()
		go func() {
			defer wg.Done()
			bs.RecordSuccess()
		}()
	}

	wg.Wait()
	// race 플래그로 데이터 레이스 탐지 — PASS이면 동시성 안전
}

func TestFetchPage_504RetryNotBlockedByTransientCooldown(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := int(attempts.Add(1))
		if idx == 1 {
			w.WriteHeader(http.StatusGatewayTimeout) // 504
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
		}
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.fetchPage(ctx, server.URL)
	require.NoError(t, err, "504->200 should succeed (transient backoff must not block retry)")
	assert.Contains(t, result, "ytInitialData")
	assert.Equal(t, int32(2), attempts.Load(), "should have exactly 2 attempts")
}

func TestFetchPageOnce_HardCooldownOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	// transient cooldown을 인위적으로 설정
	client.backoffState.RecordTransientError()
	assert.Greater(t, client.backoffState.TransientCooldownRemaining(), time.Duration(0))

	ctx := context.Background()
	// fetchPageOnce는 transient cooldown에서 ErrRateLimited를 반환하지 않아야 함
	result, err := client.fetchPageOnce(ctx, server.URL)
	require.NoError(t, err, "fetchPageOnce should not be blocked by transient cooldown")
	assert.Contains(t, result, "ok")
}

// --- Phase 4 테스트: RateLimiter ---

func TestRateLimiter_ConcurrentSlots(t *testing.T) {
	rl := NewRateLimiter(100 * time.Millisecond)
	ctx := context.Background()

	var wg sync.WaitGroup
	completionTimes := make([]time.Time, 3)

	for i := range 3 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = rl.Wait(ctx)
			completionTimes[idx] = time.Now()
		}(i)
	}

	wg.Wait()
	sort.Slice(completionTimes, func(i, j int) bool {
		return completionTimes[i].Before(completionTimes[j])
	})

	totalSpan := completionTimes[2].Sub(completionTimes[0])
	assert.GreaterOrEqual(t, totalSpan, 180*time.Millisecond, "total span should be at least 180ms")

	for i := 1; i < len(completionTimes); i++ {
		gap := completionTimes[i].Sub(completionTimes[i-1])
		assert.GreaterOrEqual(t, gap, 80*time.Millisecond, "adjacent gap should be at least 80ms")
	}
}

func TestRateLimiter_ContextCancel(t *testing.T) {
	rl := NewRateLimiter(10 * time.Second)
	ctx := context.Background()

	// 첫 호출: 즉시 반환
	err := rl.Wait(ctx)
	require.NoError(t, err)

	// 두 번째 호출: 취소된 context
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // 즉시 취소

	err = rl.Wait(cancelCtx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRateLimiter_CancelDoesNotConsumeSlot(t *testing.T) {
	rl := NewRateLimiter(50 * time.Millisecond)
	ctx := context.Background()

	// 첫 호출: 즉시 반환, lastTime 설정
	err := rl.Wait(ctx)
	require.NoError(t, err)

	// 두 번째 호출: 취소된 context → slot rollback
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = rl.Wait(cancelCtx)
	require.Error(t, err)

	// 50ms 대기 후 다음 호출: rollback 덕분에 즉시 반환 가능
	time.Sleep(60 * time.Millisecond)
	start := time.Now()
	err = rl.Wait(ctx)
	require.NoError(t, err)
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 20*time.Millisecond, "Wait should return immediately after rollback + interval")
}

func TestRateLimiter_ConcurrentCancelStress(t *testing.T) {
	rl := NewRateLimiter(50 * time.Millisecond)
	ctx := context.Background()

	err := rl.Wait(ctx)
	require.NoError(t, err)

	const concurrentCancels = 10

	startCh := make(chan struct{})
	errCh := make(chan error, concurrentCancels)
	var wg sync.WaitGroup

	for range concurrentCancels {
		wg.Go(func() {
			cancelCtx, cancel := context.WithCancel(context.Background())
			cancel()
			<-startCh
			errCh <- rl.Wait(cancelCtx)
		})
	}

	close(startCh)
	wg.Wait()
	close(errCh)

	for waitErr := range errCh {
		require.Error(t, waitErr)
		assert.ErrorIs(t, waitErr, context.Canceled)
	}

	time.Sleep(60 * time.Millisecond)
	start := time.Now()
	err = rl.Wait(ctx)
	require.NoError(t, err)
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 20*time.Millisecond, "concurrent canceled waiters should not consume future slots")
}

// --- Phase 5 테스트: 포인터 동일성 ---

func TestSharedRL_PointerIdentity(t *testing.T) {
	rl := NewRateLimiter(3 * time.Second)

	c1 := NewClient(WithRateLimiter(rl), WithUAProvider(ua.NewStaticProvider("a")))
	c2 := NewClient(WithRateLimiter(rl), WithUAProvider(ua.NewStaticProvider("b")))
	c3 := NewClient(WithRateLimiter(rl), WithUAProvider(ua.NewStaticProvider("c")))

	require.Same(t, c1.rateLimiter, c2.rateLimiter, "c1 and c2 should share same RateLimiter")
	require.Same(t, c2.rateLimiter, c3.rateLimiter, "c2 and c3 should share same RateLimiter")
}

// --- 회귀 테스트: transient backoff 교차 오염 방지 ---

func TestFetchPage_ConcurrentTransientErrors_NoAmplification(t *testing.T) {
	// 2개 동시 fetchPage × 3 retry = 6 HTTP 요청이지만 transientErrors는 2여야 함
	// 수정 전: per-request RecordTransientError → transientErrors=4~6 (교차 오염)
	// 수정 후: per-operation RecordTransientError → transientErrors=2 (정확)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			_, _ = client.fetchPage(ctx, server.URL)
		}()
	}
	wg.Wait()

	client.backoffState.mu.Lock()
	actualErrors := client.backoffState.transientErrors
	client.backoffState.mu.Unlock()

	assert.Equal(t, 2, actualErrors,
		"transientErrors should equal fetchPage call count (2), not total HTTP attempts")
}

func TestFetchPage_ContextCancel_NoTransientRecord(t *testing.T) {
	// context 취소 시 transientErrors가 기록되지 않음을 검증
	// retry 라이브러리가 context cancel 시에도 lastErr(httpStatusError)를 반환하므로
	// ctx.Err() 가드 없이는 잘못된 transient 에러 기록이 발생함
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	// 500ms 타임아웃: 첫 502 응답 후 retry sleep 중 만료됨
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := client.fetchPage(ctx, server.URL)
	require.Error(t, err)

	client.backoffState.mu.Lock()
	actualErrors := client.backoffState.transientErrors
	client.backoffState.mu.Unlock()

	assert.Equal(t, 0, actualErrors,
		"transientErrors should not be recorded when context is cancelled")
}

func TestFetchPageOnce_Headers(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
	}))
	defer server.Close()

	// Chrome UA로 테스트 (Client Hints 포함해야 함)
	chromeUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/141.0.0.0 Safari/537.36"
	// StaticProvider는 CH를 생성하지 않으므로, RotatingProvider 대신 직접 snap을 검증할 수 없음
	// 대신 StaticProvider로 기본 헤더만 검증
	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider(chromeUA)),
	)

	ctx := context.Background()
	_, err := client.fetchPageOnce(ctx, server.URL)
	require.NoError(t, err)

	// 필수 헤더 검증
	assert.Equal(t, "SOCS=CAI", receivedHeaders.Get("Cookie"))
	assert.Equal(t, "document", receivedHeaders.Get("Sec-Fetch-Dest"))
	assert.Equal(t, "navigate", receivedHeaders.Get("Sec-Fetch-Mode"))
	assert.Equal(t, "none", receivedHeaders.Get("Sec-Fetch-Site"))
	assert.Equal(t, "?1", receivedHeaders.Get("Sec-Fetch-User"))
	assert.Equal(t, "1", receivedHeaders.Get("Upgrade-Insecure-Requests"))
	assert.Equal(t, "max-age=0", receivedHeaders.Get("Cache-Control"))
	assert.Contains(t, receivedHeaders.Get("Accept"), "text/html")
	assert.Equal(t, chromeUA, receivedHeaders.Get("User-Agent"))

	// StaticProvider는 CH를 설정하지 않으므로 Sec-CH-UA는 비어야 함
	assert.Empty(t, receivedHeaders.Get("Sec-CH-UA"), "StaticProvider should not set Sec-CH-UA")
}

func TestFetchPageOnce_ClientHintsHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ytInitialData = {};</html>"))
	}))
	defer server.Close()

	snap := ua.HeaderSnapshot{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/142.0.0.0 Safari/537.36",
		SecChUA:         "\"Chromium\";v=\"142\", \"Not=A?Brand\";v=\"24\"",
		SecChUAPlatform: "\"Windows\"",
		Accept:          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	}

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(&fixedSnapshotProvider{snap: snap}),
	)

	ctx := context.Background()
	_, err := client.fetchPageOnce(ctx, server.URL)
	require.NoError(t, err)

	assert.Equal(t, snap.UserAgent, receivedHeaders.Get("User-Agent"))
	assert.Equal(t, snap.Accept, receivedHeaders.Get("Accept"))
	assert.Equal(t, snap.SecChUA, receivedHeaders.Get("Sec-CH-UA"))
	assert.Equal(t, "?0", receivedHeaders.Get("Sec-CH-UA-Mobile"))
	assert.Equal(t, snap.SecChUAPlatform, receivedHeaders.Get("Sec-CH-UA-Platform"))
}
