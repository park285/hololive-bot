package settings

import (
	"log/slog"

	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
)

func ApplyScraperProxyToggle(
	enabled bool,
	youtubeService youtube.Service,
	holodexService *holodexprovider.Service,
	scraperScheduler *scheduler.Scheduler,
	logger *slog.Logger,
) {
	youtubeApplied := false
	holodexApplied := false
	schedulerApplied := 0

	if youtubeService != nil {
		youtubeApplied = youtubeService.SetScraperProxyEnabled(enabled)
	}

	if holodexService != nil {
		holodexApplied = holodexService.SetScraperProxyEnabled(enabled)
	}

	if scraperScheduler != nil {
		schedulerApplied = scraperScheduler.SetProxyEnabled(enabled)
	}

	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("Applied scraper proxy toggle",
		slog.Bool("enabled", enabled),
		slog.Bool("youtube_applied", youtubeApplied),
		slog.Bool("holodex_applied", holodexApplied),
		slog.Int("scheduler_pollers_applied", schedulerApplied),
	)
}
