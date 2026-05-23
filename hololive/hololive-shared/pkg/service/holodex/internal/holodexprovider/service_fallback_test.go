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

package holodexprovider

import (
	"context"
	"fmt"
	sharedjson "github.com/park285/hololive-bot/shared-go/pkg/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	ytscraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func newInMemoryCacheClient() *cachemocks.Client {
	var mu sync.Mutex
	store := make(map[string][]byte)

	return &cachemocks.Client{
		GetFunc: func(_ context.Context, key string, dest any) error {
			mu.Lock()
			payload, ok := store[key]
			mu.Unlock()
			if !ok {
				return nil
			}
			return sharedjson.Unmarshal(payload, dest)
		},
		SetFunc: func(_ context.Context, key string, value any, _ time.Duration) error {
			payload, err := sharedjson.Marshal(value)
			if err != nil {
				return err
			}
			mu.Lock()
			store[key] = payload
			mu.Unlock()
			return nil
		},
	}
}

func newServiceForFallbackTest(requester Requester) *Service {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cacheClient := newInMemoryCacheClient()
	return &Service{
		requester:    requester,
		logger:       logger,
		cacheManager: NewCacheManager(cacheClient, logger),
		mapper:       NewStreamMapper(logger),
		filter:       NewStreamFilter(logger),
	}
}

func newServiceForFallbackTestWithScraper(requester Requester, scraperService *ScraperService) *Service {
	service := newServiceForFallbackTest(requester)
	service.scraper = scraperService
	return service
}

func TestGetChannels_FallbackWorkerPoolLimitsConcurrency(t *testing.T) {
	var (
		inFlight       int32
		maxInFlight    int32
		channelReqsCnt int32
	)
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path == "/channels" {
				return nil, &APIError{
					Operation:  "list_channels",
					StatusCode: http.StatusServiceUnavailable,
					Err:        fmt.Errorf("forced /channels failure"),
				}
			}

			if !strings.HasPrefix(path, "/channels/") {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}

			channelID := strings.TrimPrefix(path, "/channels/")
			current := atomic.AddInt32(&inFlight, 1)
			atomic.AddInt32(&channelReqsCnt, 1)
			for {
				previous := atomic.LoadInt32(&maxInFlight)
				if current <= previous || atomic.CompareAndSwapInt32(&maxInFlight, previous, current) {
					break
				}
			}

			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)

			return fmt.Appendf(nil, `{"id":"%s","name":"%s"}`, channelID, channelID), nil
		},
	}

	service := newServiceForFallbackTest(mockReq)
	channelIDs := []string{
		"c01", "c02", "c03", "c04", "c05", "c06",
		"c07", "c08", "c09", "c10", "c11", "c12",
	}

	got, err := service.GetChannels(context.Background(), channelIDs)
	if err != nil {
		t.Fatalf("GetChannels() error = %v", err)
	}

	if len(got) != len(channelIDs) {
		t.Fatalf("GetChannels() len = %d, want %d", len(got), len(channelIDs))
	}

	for _, id := range channelIDs {
		if _, ok := got[id]; !ok {
			t.Fatalf("GetChannels() missing channel id=%s", id)
		}
	}

	if max := atomic.LoadInt32(&maxInFlight); max > 5 {
		t.Fatalf("fallback max concurrency = %d, want <= 5", max)
	}

	if gotReqs := atomic.LoadInt32(&channelReqsCnt); int(gotReqs) != len(channelIDs) {
		t.Fatalf("fallback request count = %d, want %d", gotReqs, len(channelIDs))
	}
}

func TestGetChannels_FallbackStopsWhenContextCanceled(t *testing.T) {
	var fallbackChannelReqs int32
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path == "/channels" {
				return nil, context.Canceled
			}
			if strings.HasPrefix(path, "/channels/") {
				atomic.AddInt32(&fallbackChannelReqs, 1)
				return nil, context.Canceled
			}
			return nil, fmt.Errorf("unexpected path: %s", path)
		},
	}

	service := newServiceForFallbackTest(mockReq)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.GetChannels(ctx, []string{"c1", "c2", "c3"})
	if err == nil {
		t.Fatal("GetChannels() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "get channels batch list") {
		t.Fatalf("GetChannels() error = %v, want contains %q", err, "get channels batch list")
	}
	if got := atomic.LoadInt32(&fallbackChannelReqs); got != 0 {
		t.Fatalf("fallback channel request count = %d, want 0", got)
	}
}

func TestCollectIndividualChannelFetchResultsReturnsOnCancel(t *testing.T) {
	service := newServiceForFallbackTest(&MockRequester{})
	ctx, cancel := context.WithCancel(context.Background())
	resultChan := make(chan channelFetchResult)
	done := make(chan error, 1)

	go func() {
		_, err := service.collectIndividualChannelFetchResults(ctx, []string{"c1"}, map[string]*domain.Channel{}, resultChan)
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("collectIndividualChannelFetchResults() error = nil, want non-nil")
		}
		if !strings.Contains(err.Error(), "batch channel fetch canceled") {
			t.Fatalf("collectIndividualChannelFetchResults() error = %v, want canceled batch error", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("collectIndividualChannelFetchResults() did not return after context cancellation")
	}
}

func TestGetChannels_DoesNotFallbackOnNonRetryableListError(t *testing.T) {
	var fallbackChannelReqs int32
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path == "/channels" {
				return nil, &APIError{
					Operation:  "list_channels",
					StatusCode: http.StatusBadRequest,
					Err:        fmt.Errorf("bad request"),
				}
			}
			if strings.HasPrefix(path, "/channels/") {
				atomic.AddInt32(&fallbackChannelReqs, 1)
				return []byte(`{"id":"c1","name":"c1"}`), nil
			}
			return nil, fmt.Errorf("unexpected path: %s", path)
		},
	}

	service := newServiceForFallbackTest(mockReq)

	got, err := service.GetChannels(context.Background(), []string{"c1", "c2"})
	if err == nil {
		t.Fatal("GetChannels() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "get channels batch list") {
		t.Fatalf("GetChannels() error = %v, want contains %q", err, "get channels batch list")
	}
	if len(got) != 0 {
		t.Fatalf("GetChannels() len = %d, want 0", len(got))
	}
	if gotReqs := atomic.LoadInt32(&fallbackChannelReqs); gotReqs != 0 {
		t.Fatalf("fallback channel request count = %d, want 0", gotReqs)
	}
}

func TestGetChannelsLiveStatus_UsesYouTubeProducerWithoutOfficialScheduleFallback(t *testing.T) {
	var officialRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		officialRequests.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	startUnix := time.Now().UTC().Add(10 * time.Minute).Unix()
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/users/live" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			return nil, &APIError{
				Operation:  "channels_live_status",
				StatusCode: http.StatusServiceUnavailable,
				Err:        fmt.Errorf("upstream unavailable"),
			}
		},
	}

	scraperService := newScraperServiceForTest(server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)), server.URL, func(_ context.Context, channelID string) ([]*ytscraper.UpcomingEvent, error) {
		switch channelID {
		case "c1":
			return []*ytscraper.UpcomingEvent{
				{
					VideoID:      "video-1",
					Title:        "stream-1",
					Status:       "UPCOMING",
					StartTime:    &startUnix,
					ChannelTitle: "channel-1",
				},
			}, nil
		case "c2":
			return []*ytscraper.UpcomingEvent{}, nil
		default:
			return nil, fmt.Errorf("unexpected channel: %s", channelID)
		}
	},
	)

	service := newServiceForFallbackTestWithScraper(mockReq, scraperService)

	streams, err := service.GetChannelsLiveStatus(context.Background(), []string{"c1", "c2"})
	if err != nil {
		t.Fatalf("GetChannelsLiveStatus() error = %v", err)
	}
	if len(streams) != 1 {
		t.Fatalf("len(streams) = %d, want 1", len(streams))
	}
	if streams[0].ChannelID != "c1" {
		t.Fatalf("channel_id = %s, want c1", streams[0].ChannelID)
	}
	if got := officialRequests.Load(); got != 0 {
		t.Fatalf("official schedule requests = %d, want 0", got)
	}
}

func TestGetChannelsLiveStatus_DoesNotFallbackOnNonRetryableError(t *testing.T) {
	var scraperCalls atomic.Int32
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/users/live" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			return nil, &APIError{
				Operation:  "channels_live_status",
				StatusCode: http.StatusBadRequest,
				Err:        fmt.Errorf("bad request"),
			}
		},
	}
	scraperService := newScraperServiceForTest(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "http://example.invalid", func(_ context.Context, channelID string) ([]*ytscraper.UpcomingEvent, error) {
		scraperCalls.Add(1)
		return nil, fmt.Errorf("unexpected scraper call for %s", channelID)
	},
	)

	service := newServiceForFallbackTestWithScraper(mockReq, scraperService)

	streams, err := service.GetChannelsLiveStatus(context.Background(), []string{"c1", "c2"})
	if err == nil {
		t.Fatal("GetChannelsLiveStatus() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "get channels live status") {
		t.Fatalf("GetChannelsLiveStatus() error = %v, want contains %q", err, "get channels live status")
	}
	if len(streams) != 0 {
		t.Fatalf("len(streams) = %d, want 0", len(streams))
	}
	if got := scraperCalls.Load(); got != 0 {
		t.Fatalf("scraper calls = %d, want 0", got)
	}
}

func TestGetChannel_DoesNotFallbackOnNonRetryableAPIError(t *testing.T) {
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/channels/c1" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			return nil, &APIError{
				Operation:  "get_channel",
				StatusCode: http.StatusBadRequest,
				Err:        fmt.Errorf("bad request"),
			}
		},
	}
	scraperService := newScraperServiceForTest(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "", nil)

	service := newServiceForFallbackTestWithScraper(mockReq, scraperService)

	channel, err := service.GetChannel(context.Background(), "c1")
	if err == nil {
		t.Fatal("GetChannel() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "get channel") {
		t.Fatalf("GetChannel() error = %v, want contains %q", err, "get channel")
	}
	if strings.Contains(err.Error(), "scraper fallback failed") {
		t.Fatalf("GetChannel() error = %v, want no scraper fallback attempt", err)
	}
	if channel != nil {
		t.Fatalf("GetChannel() channel = %#v, want nil", channel)
	}
}

type mockTimeoutError struct {
	msg     string
	timeout bool
}

func (e *mockTimeoutError) Error() string   { return e.msg }
func (e *mockTimeoutError) Timeout() bool   { return e.timeout }
func (e *mockTimeoutError) Temporary() bool { return false }

func TestShouldUseFallbackTimeout(t *testing.T) {
	service := newServiceForFallbackTest(&MockRequester{})

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
			got := service.shouldUseFallback(tt.ctx, tt.err)
			if got != tt.expected {
				t.Errorf("shouldUseFallback(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestShouldUseFallbackCallerContextExpired(t *testing.T) {
	service := newServiceForFallbackTest(&MockRequester{})

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if service.shouldUseFallback(canceledCtx, context.DeadlineExceeded) {
		t.Error("호출자 context 만료 시 폴백하면 안 됨")
	}

	if service.shouldUseFallback(canceledCtx, &mockTimeoutError{msg: "timeout", timeout: true}) {
		t.Error("호출자 context 만료 시 net timeout도 폴백하면 안 됨")
	}
}

func TestGetChannel_ReturnsErrorWhenRetryableFallbackAlsoFails(t *testing.T) {
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != "GET" {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}
			if path != "/channels/c1" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			return nil, &APIError{
				Operation:  "get_channel",
				StatusCode: http.StatusServiceUnavailable,
				Err:        fmt.Errorf("upstream unavailable"),
			}
		},
	}
	scraperService := newScraperServiceForTest(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), "", nil)

	service := newServiceForFallbackTestWithScraper(mockReq, scraperService)

	channel, err := service.GetChannel(context.Background(), "c1")
	if err == nil {
		t.Fatal("GetChannel() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "primary and scraper fallback failed") {
		t.Fatalf("GetChannel() error = %v, want contains %q", err, "primary and scraper fallback failed")
	}
	if channel != nil {
		t.Fatalf("GetChannel() channel = %#v, want nil", channel)
	}
}
