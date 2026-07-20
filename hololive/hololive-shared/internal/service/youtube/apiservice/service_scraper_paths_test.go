package apiservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type stubScraper struct {
	recentVideos map[string][]*scraper.Video
	recentErr    map[string]error
	channelStats map[string]*scraper.ChannelStats
	statsErr     map[string]error
}

func (s *stubScraper) GetRecentVideos(_ context.Context, channelID string, _ int) ([]*scraper.Video, error) {
	if err := s.recentErr[channelID]; err != nil {
		return nil, err
	}
	return s.recentVideos[channelID], nil
}

func (s *stubScraper) GetChannelStats(_ context.Context, channelID string) (*scraper.ChannelStats, error) {
	if err := s.statsErr[channelID]; err != nil {
		return nil, err
	}
	if cs, ok := s.channelStats[channelID]; ok {
		return cs, nil
	}
	return nil, fmt.Errorf("stub: no stats for %s", channelID)
}

func (s *stubScraper) SetProxyEnabled(bool) bool { return false }
func (s *stubScraper) ProxyEnabled() bool        { return false }

func newScraperPathService(s scraperClient) *serviceImpl {
	return &serviceImpl{
		scraper:       s,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		channelToName: make(map[string]string),
	}
}

func TestGetRecentVideos_ScraperPaths(t *testing.T) {
	t.Parallel()

	t.Run("returns video IDs on scraper success", func(t *testing.T) {
		t.Parallel()
		ys := newScraperPathService(&stubScraper{
			recentVideos: map[string][]*scraper.Video{
				"UC1": {{VideoID: "v1"}, {VideoID: "v2"}},
			},
		})
		got, err := ys.GetRecentVideos(context.Background(), "UC1", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0] != "v1" || got[1] != "v2" {
			t.Fatalf("got %v, want [v1 v2]", got)
		}
	})

	t.Run("empty scraper result returns empty slice without error", func(t *testing.T) {
		t.Parallel()
		ys := newScraperPathService(&stubScraper{
			recentVideos: map[string][]*scraper.Video{"UC1": {}},
		})
		got, err := ys.GetRecentVideos(context.Background(), "UC1", 10)
		if err != nil {
			t.Fatalf("empty scraper result must not error, got %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("got %v, want empty", got)
		}
	})

	t.Run("scraper error is wrapped with channel ID", func(t *testing.T) {
		t.Parallel()
		ys := newScraperPathService(&stubScraper{
			recentErr: map[string]error{"UC1": errors.New("scrape boom")},
		})
		_, err := ys.GetRecentVideos(context.Background(), "UC1", 10)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		for _, want := range []string{"UC1", "scrape boom"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q missing %q", err, want)
			}
		}
	})
}

func TestGetChannelStatistics_ScraperPaths(t *testing.T) {
	t.Parallel()

	t.Run("all channels succeed returns full map", func(t *testing.T) {
		t.Parallel()
		ys := newScraperPathService(&stubScraper{
			channelStats: map[string]*scraper.ChannelStats{
				"UC1": {ChannelID: "UC1", SubscriberCount: 100, VideoCount: 10, ViewCount: 1000},
				"UC2": {ChannelID: "UC2", SubscriberCount: 200, VideoCount: 20, ViewCount: 2000},
			},
		})
		got, err := ys.GetChannelStatistics(context.Background(), []string{"UC1", "UC2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d results, want 2", len(got))
		}
	})

	t.Run("partial failure returns partial map without error", func(t *testing.T) {
		t.Parallel()
		ys := newScraperPathService(&stubScraper{
			channelStats: map[string]*scraper.ChannelStats{
				"UC1": {ChannelID: "UC1", SubscriberCount: 100, VideoCount: 10, ViewCount: 1000},
			},
			statsErr: map[string]error{"UC2": errors.New("scrape fail")},
		})
		got, err := ys.GetChannelStatistics(context.Background(), []string{"UC1", "UC2"})
		if err != nil {
			t.Fatalf("partial success must not error, got %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d results, want 1 (partial)", len(got))
		}
		if _, ok := got["UC1"]; !ok {
			t.Fatal("UC1 should be present in partial result")
		}
	})

	t.Run("all channels fail returns error", func(t *testing.T) {
		t.Parallel()
		ys := newScraperPathService(&stubScraper{
			statsErr: map[string]error{
				"UC1": errors.New("fail1"),
				"UC2": errors.New("fail2"),
			},
		})
		_, err := ys.GetChannelStatistics(context.Background(), []string{"UC1", "UC2"})
		if err == nil {
			t.Fatal("all-fail must return error")
		}
		if !strings.Contains(err.Error(), "scraper failed for all") {
			t.Fatalf("error %q missing 'scraper failed for all'", err)
		}
	})
}
