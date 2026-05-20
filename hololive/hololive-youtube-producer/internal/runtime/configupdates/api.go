package configupdates

import (
	"log/slog"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

func BuildSubscriber(
	cacheService cache.Client,
	settingsService settings.ReadWriter,
	holodexService *holodex.Service,
	ytStack *providers.YouTubeStack,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	return buildYouTubeProducerConfigSubscriber(cacheService, settingsService, holodexService, ytStack, scraperScheduler, logger)
}
