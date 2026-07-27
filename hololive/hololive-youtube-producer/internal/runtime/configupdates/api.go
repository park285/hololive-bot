package configupdates

import (
	"log/slog"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"

	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
)

func BuildSubscriber(
	cacheService cache.Client,
	settingsService settings.ReadWriter,
	holodexService *holodexprovider.Service,
	ytStack *providers.YouTubeStack,
	scraperScheduler *scheduler.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	return buildYouTubeProducerConfigSubscriber(cacheService, settingsService, holodexService, ytStack, scraperScheduler, logger)
}
