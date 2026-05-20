package producerruntime

import (
	"log/slog"

	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/configupdates"
)

func buildRuntimeConfigSubscriber(
	features ingestionRuntimeFeatures,
	infra *youtubeProducerInfrastructure,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	if !features.youtubeEnabled && !features.photoSyncEnabled {
		return nil
	}

	configSubscriber := configupdates.BuildSubscriber(
		infra.cacheService,
		infra.settingsService,
		infra.holodexService,
		infra.ytStack,
		scraperScheduler,
		logger,
	)

	desiredProxyState := infra.settingsService.Get().ScraperProxyEnabled
	sharedsettings.ApplyScraperProxyToggle(
		desiredProxyState,
		infra.ytStack.GetService(),
		infra.holodexService,
		scraperScheduler,
		logger,
	)

	return configSubscriber
}
