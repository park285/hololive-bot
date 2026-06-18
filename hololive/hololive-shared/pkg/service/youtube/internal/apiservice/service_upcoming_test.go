package apiservice

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func testUpcomingLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestConvertScrapedEvents_EmptyEventsReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	service := &serviceImpl{logger: testUpcomingLogger(), channelToName: make(map[string]string)}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("convertScrapedEvents panicked on empty input: %v", r)
		}
	}()

	streams := service.convertScrapedEvents(nil, "UC1")
	if len(streams) != 0 {
		t.Fatalf("convertScrapedEvents(nil) returned %d streams, want 0", len(streams))
	}
}

func TestGetUpcomingStreams_ReturnsCachedStreamsWithoutScraperFallback(t *testing.T) {
	t.Parallel()

	var requestedKey string
	cached := []*domain.Stream{{ID: "cached"}}
	cacheClient := &cachemocks.Client{
		GetStreamsFunc: func(_ context.Context, key string) ([]*domain.Stream, bool) {
			requestedKey = key
			return cached, true
		},
	}

	service := &serviceImpl{
		cache:         cacheClient,
		logger:        testUpcomingLogger(),
		channelToName: make(map[string]string),
	}

	streams, err := service.GetUpcomingStreams(context.Background(), []string{"channel-b", "channel-a"})
	if err != nil {
		t.Fatalf("GetUpcomingStreams() error = %v", err)
	}
	if !reflect.DeepEqual(streams, cached) {
		t.Fatalf("GetUpcomingStreams() streams = %#v, want %#v", streams, cached)
	}

	wantKey := upcomingCacheKey([]string{"channel-b", "channel-a"})
	if requestedKey != wantKey {
		t.Fatalf("GetUpcomingStreams() cache key = %q, want %q", requestedKey, wantKey)
	}
}

func TestCompleteUpcomingAPIFallback_QuotaBlockedReturnsPartialResults(t *testing.T) {
	t.Parallel()

	var cachedKey string
	var cachedStreams []*domain.Stream
	cacheClient := &cachemocks.Client{
		SetStreamsFunc: func(_ context.Context, key string, streams []*domain.Stream, ttl time.Duration) {
			cachedKey = key
			cachedStreams = streams
			if ttl != config.DefaultYouTubeOperationalConfig().CacheExpiration {
				t.Fatalf("SetStreams() ttl = %v, want %v", ttl, config.DefaultYouTubeOperationalConfig().CacheExpiration)
			}
		},
	}

	service := &serviceImpl{
		cache:         cacheClient,
		logger:        testUpcomingLogger(),
		quotaUsed:     config.DefaultYouTubeOperationalConfig().DailyQuotaLimit,
		quotaReset:    time.Now().Add(time.Hour),
		channelToName: make(map[string]string),
	}

	wantStreams := []*domain.Stream{{ID: "scraped"}}
	got, err := service.completeUpcomingAPIFallback(context.Background(), "cache-key", &upcomingScrapeResult{
		streams:   wantStreams,
		failedIDs: []string{"UC1"},
	})
	if err != nil {
		t.Fatalf("completeUpcomingAPIFallback() error = %v", err)
	}
	if !reflect.DeepEqual(got, wantStreams) {
		t.Fatalf("completeUpcomingAPIFallback() streams = %#v, want %#v", got, wantStreams)
	}
	if cachedKey != "cache-key" {
		t.Fatalf("completeUpcomingAPIFallback() cached key = %q, want %q", cachedKey, "cache-key")
	}
	if !reflect.DeepEqual(cachedStreams, wantStreams) {
		t.Fatalf("completeUpcomingAPIFallback() cached streams = %#v, want %#v", cachedStreams, wantStreams)
	}
}

func TestCompleteUpcomingAPIFallback_QuotaBlockedWithoutPartialResultsReturnsError(t *testing.T) {
	t.Parallel()

	service := &serviceImpl{
		cache:         &cachemocks.Client{},
		logger:        testUpcomingLogger(),
		quotaUsed:     config.DefaultYouTubeOperationalConfig().DailyQuotaLimit,
		quotaReset:    time.Now().Add(time.Hour),
		channelToName: make(map[string]string),
	}

	_, err := service.completeUpcomingAPIFallback(context.Background(), "cache-key", &upcomingScrapeResult{
		failedIDs: []string{"UC1"},
	})
	if err == nil {
		t.Fatal("completeUpcomingAPIFallback() expected quota-blocked error, got nil")
	}
	if !strings.Contains(err.Error(), "api fallback blocked") {
		t.Fatalf("completeUpcomingAPIFallback() error = %v, want quota-blocked message", err)
	}
}

func TestExtractThumbnail_PrefersLargestAvailable(t *testing.T) {
	t.Parallel()

	got := extractThumbnail(&youtube.ThumbnailDetails{
		High:   &youtube.Thumbnail{Url: "high"},
		Maxres: &youtube.Thumbnail{Url: "maxres"},
	})
	if got == nil || *got != "maxres" {
		t.Fatalf("extractThumbnail() = %v, want maxres", got)
	}
}

func TestUpcomingCacheKey_SortsChannelIDs(t *testing.T) {
	t.Parallel()

	got := upcomingCacheKey([]string{"channel-b", "channel-a"})
	if got != "youtube:upcoming:channel-a,channel-b" {
		t.Fatalf("upcomingCacheKey() = %q", got)
	}
}

func TestConvertScrapedEvents_UsesFallbackThumbnailAndChannelTitle(t *testing.T) {
	t.Parallel()

	start := time.Now().Unix()
	service := &serviceImpl{logger: testUpcomingLogger(), channelToName: make(map[string]string)}

	streams := service.convertScrapedEvents([]*scraper.UpcomingEvent{
		{
			VideoID:      "video-1",
			Title:        "Upcoming stream",
			ChannelTitle: "Fallback channel",
			Status:       "UPCOMING",
			StartTime:    &start,
		},
		{
			VideoID: "video-2",
			Title:   "Ended stream",
			Status:  "ENDED",
		},
	}, "UC1")

	if len(streams) != 1 {
		t.Fatalf("convertScrapedEvents() returned %d streams, want 1", len(streams))
	}
	if streams[0].ChannelName != "Fallback channel" {
		t.Fatalf("convertScrapedEvents() channel name = %q, want fallback title", streams[0].ChannelName)
	}
	if streams[0].Thumbnail == nil || !strings.Contains(*streams[0].Thumbnail, "video-1") {
		t.Fatalf("convertScrapedEvents() thumbnail = %v, want fallback video thumbnail", streams[0].Thumbnail)
	}
	if streams[0].Status != domain.StreamStatusUpcoming {
		t.Fatalf("convertScrapedEvents() status = %v, want %v", streams[0].Status, domain.StreamStatusUpcoming)
	}
}
