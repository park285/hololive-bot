package settings

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type scraperProxyTestYouTubeService struct {
	proxyEnabled bool
	setCalls     int
}

func (s *scraperProxyTestYouTubeService) SetScraperProxyEnabled(enabled bool) bool {
	s.proxyEnabled = enabled
	s.setCalls++
	return true
}

func (s *scraperProxyTestYouTubeService) ScraperProxyEnabled() bool { return s.proxyEnabled }

func (s *scraperProxyTestYouTubeService) GetChannelStatistics(context.Context, []string) (map[string]*youtube.ChannelStats, error) {
	return map[string]*youtube.ChannelStats{}, nil
}

func (s *scraperProxyTestYouTubeService) GetRecentVideos(context.Context, string, int64) ([]string, error) {
	return nil, nil
}

type scraperProxyTogglePoller struct {
	enabled bool
}

func (p *scraperProxyTogglePoller) Name() string { return "scraper-proxy-toggle" }
func (p *scraperProxyTogglePoller) Poll(context.Context, string) error {
	return nil
}
func (p *scraperProxyTogglePoller) SetProxyEnabled(enabled bool) bool {
	p.enabled = enabled
	return true
}
func (p *scraperProxyTogglePoller) ProxyEnabled() bool { return p.enabled }

func TestApplyScraperProxyToggle(t *testing.T) {
	t.Parallel()

	t.Run("applies to youtube service and scheduler", func(t *testing.T) {
		youtubeService := &scraperProxyTestYouTubeService{}
		scheduler := poller.NewScheduler(&poller.SchedulerConfig{
			WorkerCount:     1,
			RequestInterval: 0,
		})
		trackingPoller := &scraperProxyTogglePoller{}
		scheduler.Register("channel-1", trackingPoller, poller.PriorityNormal, time.Minute)

		ApplyScraperProxyToggle(true, youtubeService, nil, scheduler, slog.New(slog.DiscardHandler))
		if youtubeService.setCalls != 1 {
			t.Fatalf("SetScraperProxyEnabled calls = %d, want 1", youtubeService.setCalls)
		}
		if !youtubeService.ScraperProxyEnabled() {
			t.Fatal("youtube proxy not enabled")
		}

		enabled, known := scheduler.ProxyEnabled()
		if !known {
			t.Fatal("scheduler proxy state unknown, want known")
		}
		if !enabled {
			t.Fatal("scheduler proxy not enabled")
		}

		ApplyScraperProxyToggle(false, youtubeService, nil, scheduler, slog.New(slog.DiscardHandler))
		if youtubeService.setCalls != 2 {
			t.Fatalf("SetScraperProxyEnabled calls = %d, want 2", youtubeService.setCalls)
		}
		if youtubeService.ScraperProxyEnabled() {
			t.Fatal("youtube proxy still enabled")
		}
		enabled, known = scheduler.ProxyEnabled()
		if !known {
			t.Fatal("scheduler proxy state unknown after disable")
		}
		if enabled {
			t.Fatal("scheduler proxy still enabled")
		}
	})

	t.Run("nil dependencies do not panic", func(t *testing.T) {
		ApplyScraperProxyToggle(true, nil, nil, nil, slog.New(slog.DiscardHandler))
	})
}
