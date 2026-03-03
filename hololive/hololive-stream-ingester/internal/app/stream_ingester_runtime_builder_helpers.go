package app

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// buildStreamIngesterYouTubeComponents: stream-ingester 전용 YouTube 컴포넌트를 구성한다.
func buildStreamIngesterYouTubeComponents(
	scraperCfg config.ScraperConfig,
	postgresService database.Client,
	membersData member.DataProvider,
	cacheService cache.Client,
	irisClient iris.Client,
	templateRenderer *template.Renderer,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) (*poller.Scheduler, *outbox.Dispatcher) {
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}

	scraperScheduler := providers.ProvideScraperScheduler(
		postgresService,
		membersData,
		providers.DefaultPollerIntervals(),
		[]string{},
		scraperProxyConfig,
		sharedRL,
		cacheService,
		logger,
	)

	outboxDispatcher := outbox.NewDispatcher(
		postgresService.GetGormDB(),
		cacheService,
		irisClient,
		templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher
}
