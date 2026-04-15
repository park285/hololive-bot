package bootstrap

import (
	"context"
	"log/slog"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

func BuildBotConfigSubscriber(
	ctx context.Context,
	deps BotConfigSubscriberDependencies,
	runtimeDeps BotConfigSubscriberRuntimeDependencies,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			sharedsettings.ApplyScraperProxyToggle(payload.Enabled, runtimeDeps.YouTubeService, runtimeDeps.HolodexService, scraperScheduler, logger)
			current := deps.Settings.Get()
			current.ScraperProxyEnabled = payload.Enabled
			if err := deps.Settings.Update(current); err != nil {
				logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
			}
		},
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
			targets := runtimeDeps.AlarmCRUD.UpdateAlarmAdvanceMinutes(ctx, payload.Minutes)
			logger.Info("Applied alarm advance minutes via pub/sub",
				slog.Int("minutes", payload.Minutes),
				slog.Any("targets", targets),
			)
			current := deps.Settings.Get()
			current.AlarmAdvanceMinutes = payload.Minutes
			current.TargetMinutes = PersistedTargetMinutes(payload.Minutes, targets)
			if err := deps.Settings.Update(current); err != nil {
				logger.Warn("Failed to persist alarm_advance_minutes setting", slog.Any("error", err))
			}
		},
	})

	return configsub.New(deps.Cache.GetClient(), applyFn, logger)
}

func PersistedTargetMinutes(alarmAdvanceMinutes int, targetMinutes []int) []int {
	if len(targetMinutes) > 0 {
		return sharedchecker.ResolveConfiguredTargetMinutes(targetMinutes)
	}

	return sharedchecker.BuildRuntimeTargetMinutes(alarmAdvanceMinutes)
}
