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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apiclient "github.com/kapu/hololive-shared/internal/service/holodex/provider/apiclient"
	htmlscraper "github.com/kapu/hololive-shared/internal/service/holodex/provider/htmlscraper"
	streammapping "github.com/kapu/hololive-shared/internal/service/holodex/provider/streammapping"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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

			return jsonv2.Unmarshal(payload, dest)
		},
		SetFunc: func(_ context.Context, key string, value any, _ time.Duration) error {
			payload, err := jsonv2.Marshal(value)
			if err != nil {
				return fmt.Errorf("marshal: %w", err)
			}

			mu.Lock()

			store[key] = payload
			mu.Unlock()

			return nil
		},
	}
}

func newServiceForFallbackTest(requester apiclient.Requester) *Service {
	logger := slog.New(slog.DiscardHandler)
	cacheClient := newInMemoryCacheClient()

	return &Service{
		requester:    requester,
		logger:       logger,
		cacheManager: NewCacheManager(cacheClient, logger),
		mapper:       streammapping.NewStreamMapper(logger),
		filter:       streammapping.NewStreamFilter(logger),
	}
}

func newServiceForFallbackTestWithScraper(requester apiclient.Requester, scraperService *htmlscraper.Service) *Service {
	service := newServiceForFallbackTest(requester)

	service.scraper = scraperService

	return service
}

func TestGetChannels_FallbackStopsWhenContextCanceled(t *testing.T) {
	var fallbackChannelReqs atomic.Int32

	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != http.MethodGet {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path == "/channels" {
				return nil, context.Canceled
			}

			if strings.HasPrefix(path, "/channels/") {
				fallbackChannelReqs.Add(1)

				return nil, context.Canceled
			}

			return nil, fmt.Errorf("unexpected path: %s", path)
		},
	}

	service := newServiceForFallbackTest(mockReq)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := service.GetChannels(ctx, []string{"c1", "c2", "c3"})
	if err == nil {
		t.Fatal("GetChannels() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "get channels batch list") {
		t.Fatalf("GetChannels() error = %v, want contains %q", err, "get channels batch list")
	}

	if got := fallbackChannelReqs.Load(); got != 0 {
		t.Fatalf("fallback channel request count = %d, want 0", got)
	}
}

func TestGetChannels_DoesNotFallbackOnNonRetryableListError(t *testing.T) {
	var fallbackChannelReqs atomic.Int32

	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != http.MethodGet {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path == "/channels" {
				return nil, &apiclient.APIError{
					Operation:  "list_channels",
					StatusCode: http.StatusBadRequest,
					Err:        errors.New("bad request"),
				}
			}

			if strings.HasPrefix(path, "/channels/") {
				fallbackChannelReqs.Add(1)

				return []byte(`{"id":"c1","name":"c1"}`), nil
			}

			return nil, fmt.Errorf("unexpected path: %s", path)
		},
	}

	service := newServiceForFallbackTest(mockReq)

	got, err := service.GetChannels(t.Context(), []string{"c1", "c2"})
	if err == nil {
		t.Fatal("GetChannels() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "get channels batch list") {
		t.Fatalf("GetChannels() error = %v, want contains %q", err, "get channels batch list")
	}

	if len(got) != 0 {
		t.Fatalf("GetChannels() len = %d, want 0", len(got))
	}

	if gotReqs := fallbackChannelReqs.Load(); gotReqs != 0 {
		t.Fatalf("fallback channel request count = %d, want 0", gotReqs)
	}
}

func TestGetChannel_DoesNotFallbackOnNonRetryableAPIError(t *testing.T) {
	mockReq := &MockRequester{
		DoRequestFunc: func(_ context.Context, method, path string, _ url.Values) ([]byte, error) {
			if method != http.MethodGet {
				return nil, fmt.Errorf("unexpected method: %s", method)
			}

			if path != "/channels/c1" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}

			return nil, &apiclient.APIError{
				Operation:  "get_channel",
				StatusCode: http.StatusBadRequest,
				Err:        errors.New("bad request"),
			}
		},
	}
	scraperService := newScraperServiceForTest(nil, slog.New(slog.DiscardHandler), "")

	service := newServiceForFallbackTestWithScraper(mockReq, scraperService)

	channel, err := service.GetChannel(t.Context(), "c1")
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
