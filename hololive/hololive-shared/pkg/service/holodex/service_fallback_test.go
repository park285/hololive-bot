package holodex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
			return json.Unmarshal(payload, dest)
		},
		SetFunc: func(_ context.Context, key string, value any, _ time.Duration) error {
			payload, err := json.Marshal(value)
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
				return nil, fmt.Errorf("forced /channels failure")
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

			return []byte(fmt.Sprintf(`{"id":"%s","name":"%s"}`, channelID, channelID)), nil
		},
	}

	svc := newServiceForFallbackTest(mockReq)
	channelIDs := []string{
		"c01", "c02", "c03", "c04", "c05", "c06",
		"c07", "c08", "c09", "c10", "c11", "c12",
	}

	got, err := svc.GetChannels(context.Background(), channelIDs)
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

	svc := newServiceForFallbackTest(mockReq)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.GetChannels(ctx, []string{"c1", "c2", "c3"})
	if err == nil {
		t.Fatal("GetChannels() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "batch channel fetch canceled") {
		t.Fatalf("GetChannels() error = %v, want contains %q", err, "batch channel fetch canceled")
	}
	if got := atomic.LoadInt32(&fallbackChannelReqs); got != 0 {
		t.Fatalf("fallback channel request count = %d, want 0", got)
	}
}
