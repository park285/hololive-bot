package app

import (
	"log/slog"
	"os"
	"strconv"

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
	pollerRegistrations := buildStreamIngesterChannelPollerRegistrations(
		postgresService,
		scraperProxyConfig,
		sharedRL,
		cacheService,
	)

	scraperScheduler := providers.ProvideScraperScheduler(
		membersData,
		logger,
		providers.WithChannelPollerRegistrations(pollerRegistrations),
	)

	outboxDispatcher := outbox.NewDispatcher(
		postgresService.GetGormDB(),
		cacheService,
		irisClient,
		templateRenderer,
		logger,
		outboxConfigFromEnv(),
	)

	return scraperScheduler, outboxDispatcher
}

func outboxConfigFromEnv() outbox.Config {
	cfg := outbox.DefaultConfig()
	raw := os.Getenv("YOUTUBE_OUTBOX_PER_ROOM_MODE")
	if raw == "" {
		return cfg
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return cfg
	}
	cfg.PerRoomMode = enabled
	return cfg
}
