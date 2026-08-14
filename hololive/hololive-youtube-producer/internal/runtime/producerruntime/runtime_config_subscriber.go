package producerruntime

import (
	"log/slog"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/configupdates"
)

func buildRuntimeConfigSubscriber(
	features ingestionRuntimeFeatures,
	infra *youtubeProducerInfrastructure,
	scraperScheduler *scheduler.Scheduler,
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
		youtubeServiceFromStack(infra.ytStack),
		infra.holodexService,
		scraperScheduler,
		logger,
	)

	return configSubscriber
}

func youtubeServiceFromStack(ytStack *providers.YouTubeStack) youtube.Service {
	if ytStack == nil {
		return nil
	}
	return ytStack.GetService()
}
