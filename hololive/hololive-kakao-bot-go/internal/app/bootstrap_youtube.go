package app

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

func buildYouTubeComponents(scraperCfg config.ScraperConfig, deps *bot.Dependencies, infra *coreInfrastructure, logger *slog.Logger) (*poller.Scheduler, *outbox.Dispatcher) {
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}

	scraperScheduler := providers.ProvideScraperScheduler(
		deps.Postgres,
		deps.MembersData,
		providers.DefaultPollerIntervals(),
		[]string{},
		scraperProxyConfig,
		infra.sharedRL,
		deps.Cache,
		logger,
	)

	outboxDispatcher := outbox.NewDispatcher(
		deps.Postgres.GetGormDB(),
		deps.Cache,
		deps.Client,
		infra.templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher
}
