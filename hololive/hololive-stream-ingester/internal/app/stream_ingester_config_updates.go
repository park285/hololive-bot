package app

import (
	"log/slog"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

// buildStreamIngesterConfigSubscriber: stream-ingester 전용 ConfigSubscriber를 구성한다.
// alarm_advance_minutes 설정은 stream-ingester에서 불필요하므로 scraper_proxy만 처리한다.
func buildStreamIngesterConfigSubscriber(
	cacheService cache.Client,
	settingsService settings.ReadWriter,
	holodexService *holodex.Service,
	ytStack *providers.YouTubeStack,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := func(update configsub.ConfigUpdate) {
		switch update.Type {
		case contractssettings.UpdateTypeScraperProxy:
			var payload contractssettings.ScraperProxyPayloadV1
			if err := json.Unmarshal(update.Payload, &payload); err != nil {
				logger.Warn("Failed to unmarshal scraper_proxy payload", slog.Any("error", err))
				return
			}
			applyScraperProxyToggle(payload.Enabled, ProvideYouTubeService(ytStack), holodexService, scraperScheduler, logger)
			current := settingsService.Get()
			current.ScraperProxyEnabled = payload.Enabled
			if err := settingsService.Update(current); err != nil {
				logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
			}

		case contractssettings.UpdateTypeAlarmAdvanceMinutes:
			// stream-ingester는 alarm dispatch를 담당하지 않으므로 무시
			logger.Debug("Ignoring alarm_advance_minutes config update (stream-ingester)")

		default:
			logger.Warn("Unknown config update type", slog.String("type", update.Type))
		}
	}

	return configsub.New(cacheService.GetClient(), applyFn, logger)
}
