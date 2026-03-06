package app

import (
	"log/slog"
	"reflect"

	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type scraperProxyRuntimeService interface {
	SetScraperProxyEnabled(enabled bool) bool
	ScraperProxyEnabled() bool
}

func normalizeScraperProxyRuntimeService(service scraperProxyRuntimeService) scraperProxyRuntimeService {
	if service == nil {
		return nil
	}

	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func:
		if value.IsNil() {
			return nil
		}
	}

	return service
}

func applyScraperProxyToggle(
	enabled bool,
	youtubeService youtube.Service,
	holodexService scraperProxyRuntimeService,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) {
	holodexService = normalizeScraperProxyRuntimeService(holodexService)

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

	logger.Info("Applied scraper proxy toggle",
		slog.Bool("enabled", enabled),
		slog.Bool("youtube_applied", youtubeApplied),
		slog.Bool("holodex_applied", holodexApplied),
		slog.Int("scheduler_pollers_applied", schedulerApplied),
	)
}
