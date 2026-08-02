package apiservice

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/park285/shared-go/pkg/json"
)

var errScrapeFailed = errors.New("scrape failed")

type countingScraper struct {
	mu    sync.Mutex
	calls map[string]int
	stats map[string]*parser.ChannelStats
	errs  map[string]error
}

func (s *countingScraper) GetRecentVideos(context.Context, string, int) ([]*parser.Video, error) {
	return nil, nil
}

func (s *countingScraper) GetChannelStats(_ context.Context, channelID string) (*parser.ChannelStats, error) {
	s.mu.Lock()
	s.calls[channelID]++
	s.mu.Unlock()

	if err := s.errs[channelID]; err != nil {
		return nil, err
	}
	return s.stats[channelID], nil
}

func (s *countingScraper) SetProxyEnabled(bool) bool { return false }
func (s *countingScraper) ProxyEnabled() bool        { return false }

func (s *countingScraper) callCount(channelID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[channelID]
}

func newFakeStatsCache() (client *mocks.Client, writeCount func() int) {
	var (
		mu     sync.Mutex
		store  = make(map[string]string)
		writes int
	)

	client = &mocks.Client{
		Lenient: true,
		GetStringFunc: func(_ context.Context, key string) (string, bool, error) {
			mu.Lock()
			defer mu.Unlock()
			value, ok := store[key]
			return value, ok, nil
		},
		SetFunc: func(_ context.Context, key string, value any, _ time.Duration) error {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			store[key] = string(raw)
			writes++
			return nil
		},
	}

	return client, func() int {
		mu.Lock()
		defer mu.Unlock()
		return writes
	}
}

func newCachedStatsService(scraper *countingScraper, cacheClient *mocks.Client) *serviceImpl {
	return &serviceImpl{
		scraper:       scraper,
		cache:         cacheClient,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		channelToName: make(map[string]string),
	}
}

func TestGetChannelStatistics_SecondCallIsServedFromCache(t *testing.T) {
	t.Parallel()

	scraper := &countingScraper{
		calls: make(map[string]int),
		stats: map[string]*parser.ChannelStats{
			"UC1": {ChannelID: "UC1", SubscriberCount: 100, VideoCount: 10, ViewCount: 1000, Handle: "@one"},
			"UC2": {ChannelID: "UC2", SubscriberCount: 200, VideoCount: 20, ViewCount: 2000, Handle: "@two"},
		},
	}
	cacheClient, writes := newFakeStatsCache()
	ys := newCachedStatsService(scraper, cacheClient)

	first, err := ys.GetChannelStatistics(context.Background(), []string{"UC1", "UC2"})
	if err != nil {
		t.Fatalf("first GetChannelStatistics() error = %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first GetChannelStatistics() len = %d, want 2", len(first))
	}
	if got := writes(); got != 2 {
		t.Fatalf("cache writes = %d, want 2", got)
	}

	second, err := ys.GetChannelStatistics(context.Background(), []string{"UC1", "UC2"})
	if err != nil {
		t.Fatalf("second GetChannelStatistics() error = %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("second GetChannelStatistics() len = %d, want 2", len(second))
	}
	for _, channelID := range []string{"UC1", "UC2"} {
		if got := scraper.callCount(channelID); got != 1 {
			t.Fatalf("scraper calls for %s = %d, want 1 (cache hit expected)", channelID, got)
		}
		if second[channelID].SubscriberCount != first[channelID].SubscriberCount {
			t.Fatalf("cached stats for %s = %+v, want %+v", channelID, second[channelID], first[channelID])
		}
	}
}

func TestGetChannelStatistics_PartialCacheHitOnlyScrapesMissing(t *testing.T) {
	t.Parallel()

	scraper := &countingScraper{
		calls: make(map[string]int),
		stats: map[string]*parser.ChannelStats{
			"UC1": {ChannelID: "UC1", SubscriberCount: 100},
			"UC3": {ChannelID: "UC3", SubscriberCount: 300},
		},
	}
	cacheClient, _ := newFakeStatsCache()
	ys := newCachedStatsService(scraper, cacheClient)

	if _, err := ys.GetChannelStatistics(context.Background(), []string{"UC1"}); err != nil {
		t.Fatalf("warm-up GetChannelStatistics() error = %v", err)
	}

	got, err := ys.GetChannelStatistics(context.Background(), []string{"UC1", "UC3"})
	if err != nil {
		t.Fatalf("GetChannelStatistics() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetChannelStatistics() len = %d, want 2", len(got))
	}
	if calls := scraper.callCount("UC1"); calls != 1 {
		t.Fatalf("scraper calls for cached UC1 = %d, want 1", calls)
	}
	if calls := scraper.callCount("UC3"); calls != 1 {
		t.Fatalf("scraper calls for uncached UC3 = %d, want 1", calls)
	}
}

func TestGetChannelStatistics_AllScrapesFailWithoutCacheReturnsError(t *testing.T) {
	t.Parallel()

	scraper := &countingScraper{
		calls: make(map[string]int),
		errs: map[string]error{
			"UC1": errScrapeFailed,
			"UC2": errScrapeFailed,
		},
	}
	cacheClient, _ := newFakeStatsCache()
	ys := newCachedStatsService(scraper, cacheClient)

	got, err := ys.GetChannelStatistics(context.Background(), []string{"UC1", "UC2"})
	if err == nil {
		t.Fatalf("GetChannelStatistics() error = nil, want scraper failure; got %+v", got)
	}
	if got != nil {
		t.Fatalf("GetChannelStatistics() = %+v, want nil on total failure", got)
	}
}

func TestGetChannelStatistics_CachedEntrySurvivesScrapeFailure(t *testing.T) {
	t.Parallel()

	scraper := &countingScraper{
		calls: make(map[string]int),
		stats: map[string]*parser.ChannelStats{"UC1": {ChannelID: "UC1", SubscriberCount: 100}},
		errs:  map[string]error{"UC2": errScrapeFailed},
	}
	cacheClient, _ := newFakeStatsCache()
	ys := newCachedStatsService(scraper, cacheClient)

	if _, err := ys.GetChannelStatistics(context.Background(), []string{"UC1"}); err != nil {
		t.Fatalf("warm-up GetChannelStatistics() error = %v", err)
	}

	got, err := ys.GetChannelStatistics(context.Background(), []string{"UC1", "UC2"})
	if err != nil {
		t.Fatalf("GetChannelStatistics() error = %v, want cached result despite UC2 failure", err)
	}
	if len(got) != 1 || got["UC1"] == nil {
		t.Fatalf("GetChannelStatistics() = %+v, want only the cached UC1 entry", got)
	}
}
