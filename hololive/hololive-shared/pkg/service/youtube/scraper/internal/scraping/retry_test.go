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

func TestIsTimeoutOrTemporaryErrorTypedNil(t *testing.T) {
	var typedNil *flakyNetError

	require.NotPanics(t, func() {
		assert.False(t, isTimeoutOrTemporaryError(typedNil))
	})
}

func TestIsTimeoutFailureNestedTypedNil(t *testing.T) {
	var typedNil *flakyNetError
	err := &url.Error{Op: "Get", URL: "https://youtube.example", Err: typedNil}

	require.NotPanics(t, func() {
		assert.False(t, isTimeoutFailure(err))
	})
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
		expectAttempts int32
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
					mustWriteResponse(t, w, "<html>ytInitialData = {};</html>")
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

			assert.Equal(t, tt.expectAttempts, attempts.Load())

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

func TestNetHTTPPageFetcher_StatusBodyAndHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("X-Test-Header", "ok")
		w.WriteHeader(http.StatusOK)
		mustWriteResponse(t, w, "<html>ytInitialData = {};</html>")
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	body, err := client.fetchPageOnce(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Contains(t, body, "ytInitialData")
	assert.Equal(t, "test-agent", receivedHeaders.Get("User-Agent"))
	assert.Equal(t, "SOCS=CAI", receivedHeaders.Get("Cookie"))
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

func TestGetShorts_UsesSingleAttemptHighFrequencyPolicy(t *testing.T) {
	var attempts atomic.Int32
	shortsJSON := `{"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"title":"Shorts","content":{"richGridRenderer":{"contents":[]}}}}]}}}`
	shortsHTML := "<script>var ytInitialData = " + shortsJSON + ";</script>"

	client := NewClient(
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "/channel/UC_TEST/shorts", req.URL.Path)
				if attempts.Add(1) == 1 {
					return &http.Response{
						StatusCode: http.StatusBadGateway,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("bad gateway")),
						Request:    req,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(shortsHTML)),
					Request:    req,
				}, nil
			}),
		}),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	_, err := client.GetShorts(context.Background(), "UC_TEST", 10)
	require.Error(t, err)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestPublishedAtResolvers_UseSingleAttemptMetadataPolicy(t *testing.T) {
	tests := []struct {
		name     string
		call     func(*Client) (*time.Time, error)
		wantPath string
		wantBody string
	}{
		{
			name: "video resolver",
			call: func(client *Client) (*time.Time, error) {
				return client.ResolveVideoPublishedAt(context.Background(), "video-1")
			},
			wantPath: "/watch",
			wantBody: `<html><head><meta itemprop="uploadDate" content="2026-04-10T10:11:12+09:00"></head></html>`,
		},
		{
			name: "community resolver",
			call: func(client *Client) (*time.Time, error) {
				return client.ResolveCommunityPostPublishedAt(context.Background(), "post-1")
			},
			wantPath: "/post/post-1",
			wantBody: `<html><head><meta itemprop="datePublished" content="2026-04-10T10:11:12+09:00"></head></html>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32
			client := NewClient(
				WithHTTPClient(&http.Client{
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						assert.Equal(t, tt.wantPath, req.URL.Path)
						if attempts.Add(1) == 1 {
							return &http.Response{
								StatusCode: http.StatusBadGateway,
								Header:     make(http.Header),
								Body:       io.NopCloser(strings.NewReader("bad gateway")),
								Request:    req,
							}, nil
						}
						return &http.Response{
							StatusCode: http.StatusOK,
							Header:     make(http.Header),
							Body:       io.NopCloser(strings.NewReader(tt.wantBody)),
							Request:    req,
						}, nil
					}),
				}),
				WithRateLimiter(NewRateLimiter(0)),
				WithUAProvider(ua.NewStaticProvider("test-agent")),
			)

			_, err := tt.call(client)
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
		WithProxy(ProxyConfig{Enabled: true, URL: "socks5://proxy.internal:1080"}),
	)

	applied := client.SetProxyEnabled(false)
	assert.False(t, applied)
	assert.False(t, client.ProxyEnabled())
}

func TestDialSOCKS5WithContextFallback_CancelClosesConn(t *testing.T) {
	clientSide, peerSide := net.Pipe()
	tracking := newTrackingConn(clientSide)
	t.Cleanup(func() {
		mustClose(t, tracking)
		mustClose(t, peerSide)
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
		mustClose(t, tracking)
		mustClose(t, peerSide)
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

func TestBackoffState_WithCooldownJitterStaysInRange(t *testing.T) {
	for range 50 {
		bs := NewBackoffState(WithCooldownJitter(0.1))
		bs.RecordError()
		cd := bs.HardCooldownRemaining()
		base := 30 * time.Minute
		// jitter range [0.9, 1.1] base, with small wall-clock drift margin.
		assert.GreaterOrEqual(t, cd, time.Duration(float64(base)*0.85))
		assert.LessOrEqual(t, cd, time.Duration(float64(base)*1.15))
	}
}

func TestBackoffState_WithCooldownJitterDisabledMatchesBaseline(t *testing.T) {
	bs := NewBackoffState(WithCooldownJitter(0))
	bs.RecordError()
	cd := bs.HardCooldownRemaining()
	// jitter=0이면 정확히 30분 base (시계 drift 마진 2초).
	assert.InDelta(t, 30*time.Minute, cd, float64(2*time.Second))
}

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
	bs.SetTransientCooldownForTest(time.Now().Add(-1 * time.Second))
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
}

func TestFetchPage_504RetryNotBlockedByTransientCooldown(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := int(attempts.Add(1))
		if idx == 1 {
			w.WriteHeader(http.StatusGatewayTimeout) // 504
		} else {
			w.WriteHeader(http.StatusOK)
			mustWriteResponse(t, w, "<html>ytInitialData = {};</html>")
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
		mustWriteResponse(t, w, "<html>ok</html>")
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

func TestRateLimiter_ConcurrentSlots(t *testing.T) {
	rl := NewRateLimiter(100 * time.Millisecond)
	ctx := context.Background()

	var wg sync.WaitGroup
	completionTimes := make([]time.Time, 3)

	for i := range 3 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			require.NoError(t, rl.Wait(ctx))
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
	errCh := make(chan error, 2)
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			_, err := client.fetchPage(ctx, server.URL)
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.Error(t, err)
	}

	actualErrors := client.backoffState.TransientErrors()

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

	actualErrors := client.backoffState.TransientErrors()

	assert.Equal(t, 0, actualErrors,
		"transientErrors should not be recorded when context is cancelled")
}

func TestFetchPageOnce_Headers(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		mustWriteResponse(t, w, "<html>ytInitialData = {};</html>")
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
		mustWriteResponse(t, w, "<html>ytInitialData = {};</html>")
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

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)

	assert.Equal(t, 3*time.Second, parseRetryAfter("3", now))
	assert.Equal(t, time.Duration(0), parseRetryAfter("0", now))
	assert.Equal(t, time.Duration(0), parseRetryAfter("-1", now))

	httpDate := now.Add(90 * time.Second).Format(http.TimeFormat)
	assert.Equal(t, 90*time.Second, parseRetryAfter(httpDate, now))

	assert.Equal(t, time.Duration(0), parseRetryAfter("not-a-date", now))
}

func TestParseRetryAfterClampsAbsurdValues(t *testing.T) {
	now := time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)

	// 정수 초가 상한(6시간)을 초과하면 상한으로 clamp.
	assert.Equal(t, MaxRetryAfterDuration, parseRetryAfter("999999", now))

	// HTTP-date 형식도 동일하게 clamp.
	far := now.Add(48 * time.Hour).Format(http.TimeFormat)
	assert.Equal(t, MaxRetryAfterDuration, parseRetryAfter(far, now))
}

func TestIsRetryableStatusCodeIncludesThrottleLikeTransientStatuses(t *testing.T) {
	assert.True(t, isRetryableStatusCode(http.StatusRequestTimeout))
	assert.True(t, isRetryableStatusCode(http.StatusTooEarly))
	assert.True(t, isRetryableStatusCode(http.StatusBadGateway))
	assert.False(t, isRetryableStatusCode(http.StatusNotImplemented))
	assert.False(t, isRetryableStatusCode(http.StatusTooManyRequests))
}

func TestFetchPage_RateLimitUsesRetryAfterCooldown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "4000")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(
		WithHTTPClient(server.Client()),
		WithRateLimiter(NewRateLimiter(0)),
		WithUAProvider(ua.NewStaticProvider("test-agent")),
	)

	_, err := client.fetchPage(context.Background(), server.URL)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimited)

	cooldown := client.backoffState.HardCooldownRemaining()
	assert.Greater(t, cooldown, 65*time.Minute)
	assert.Less(t, cooldown, 67*time.Minute)
}

func TestBackoffState_HardRetryAfterDoesNotUndercutStagedCooldown(t *testing.T) {
	state := NewBackoffState()

	state.RecordErrorWithSuggestedCooldown(5 * time.Second)
	first := state.HardCooldownRemaining()
	assert.Greater(t, first, 29*time.Minute)

	state.RecordErrorWithSuggestedCooldown(5 * time.Second)
	second := state.HardCooldownRemaining()
	assert.Greater(t, second, 59*time.Minute)
}

func TestBackoffState_TransientRetryAfterDoesNotUndercutStagedCooldown(t *testing.T) {
	state := NewBackoffState()

	state.RecordTransientErrorWithSuggestedCooldown(1 * time.Second)
	first := state.TransientCooldownRemaining()
	assert.Greater(t, first, 29*time.Second)

	state.RecordTransientErrorWithSuggestedCooldown(1 * time.Second)
	second := state.TransientCooldownRemaining()
	assert.Greater(t, second, 179*time.Second)
}

func TestBackoffState_HardCooldownDoesNotShrinkExistingLongerDeadline(t *testing.T) {
	state := NewBackoffState()

	state.RecordErrorWithSuggestedCooldown(4 * time.Hour)
	before := state.HardCooldownRemaining()
	require.Greater(t, before, 3*time.Hour+50*time.Minute)

	time.Sleep(10 * time.Millisecond)

	state.RecordErrorWithSuggestedCooldown(5 * time.Second)
	after := state.HardCooldownRemaining()
	assert.GreaterOrEqual(t, after, before-2*time.Second)
}

func TestBackoffState_TransientCooldownDoesNotShrinkExistingLongerDeadline(t *testing.T) {
	state := NewBackoffState()

	state.RecordTransientErrorWithSuggestedCooldown(8 * time.Minute)
	before := state.TransientCooldownRemaining()
	require.Greater(t, before, 7*time.Minute+50*time.Second)

	time.Sleep(10 * time.Millisecond)

	state.RecordTransientErrorWithSuggestedCooldown(1 * time.Second)
	after := state.TransientCooldownRemaining()
	assert.GreaterOrEqual(t, after, before-2*time.Second)
}
