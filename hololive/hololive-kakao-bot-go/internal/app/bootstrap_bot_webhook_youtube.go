package app

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func buildYouTubeComponents(
	scraperCfg config.ScraperConfig,
	deps botIngestionRuntimeDependencies,
	runtimeDeps botYouTubeRuntimeDependencies,
	logger *slog.Logger,
) (*poller.Scheduler, *outbox.Dispatcher) {
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}
	pollerRegistrations := buildBotChannelPollerRegistrations(
		deps.postgres,
		scraperProxyConfig,
		runtimeDeps.sharedRateLimiter,
		deps.cache,
	)

	scraperScheduler := providers.ProvideScraperScheduler(
		deps.members,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
	)

	outboxDispatcher := outbox.NewDispatcher(
		deps.postgres.GetGormDB(),
		deps.cache,
		deps.irisClient,
		runtimeDeps.templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher
}

func buildBotWebhookHandler(
	cfg *config.Config,
	messageHandler iris.MessageHandler,
	deps botWebhookRuntimeDependencies,
	logger *slog.Logger,
) *iris.WebhookHandler {
	//nolint:contextcheck // worker goroutine은 task별 request context를 사용하므로 construction-time context 불필요
	return iris.NewWebhookHandler(cfg.Iris.WebhookToken, messageHandler, deps.cache.GetClient(), logger, iris.WebhookHandlerOptions{
		WorkerCount:    cfg.Webhook.WorkerCount,
		QueueSize:      cfg.Webhook.QueueSize,
		EnqueueTimeout: cfg.Webhook.EnqueueTimeout,
		HandlerTimeout: cfg.Webhook.HandlerTimeout,
	})
}
