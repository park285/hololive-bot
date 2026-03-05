package app

import (
	"log/slog"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

// buildBotConfigSubscriber: Bot용 ConfigSubscriber를 생성합니다.
// scraper_proxy / alarm_advance_minutes 두 가지 설정 변경을 수신하여 적용합니다.
func buildBotConfigSubscriber(
	deps botConfigSubscriberDependencies,
	runtimeDeps botConfigSubscriberRuntimeDependencies,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			applyScraperProxyToggle(payload.Enabled, runtimeDeps.youtubeService, runtimeDeps.holodexService, scraperScheduler, logger)
			// 설정 파일에도 반영
			current := deps.settings.Get()
			current.ScraperProxyEnabled = payload.Enabled
			if err := deps.settings.Update(current); err != nil {
				logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
			}
		},
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
			targets := runtimeDeps.alarmCRUD.UpdateAlarmAdvanceMinutes(payload.Minutes)
			logger.Info("Applied alarm advance minutes via pub/sub",
				slog.Int("minutes", payload.Minutes),
				slog.Any("targets", targets),
			)
			// 설정 파일에도 반영
			current := deps.settings.Get()
			current.AlarmAdvanceMinutes = payload.Minutes
			if err := deps.settings.Update(current); err != nil {
				logger.Warn("Failed to persist alarm_advance_minutes setting", slog.Any("error", err))
			}
		},
	})

	return configsub.New(deps.cache.GetClient(), applyFn, logger)
}
