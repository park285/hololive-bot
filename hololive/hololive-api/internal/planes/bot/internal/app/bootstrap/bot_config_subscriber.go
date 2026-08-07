package bootstrap

import (
	"context"
	"log/slog"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
)

func BuildBotConfigSubscriber(
	ctx context.Context,
	deps BotConfigSubscriberDependencies,
	runtimeDeps BotConfigSubscriberRuntimeDependencies,
	scraperScheduler *scheduler.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		ScraperProxy:        buildScraperProxyHandler(deps, runtimeDeps, scraperScheduler, logger),
		ACL:                 buildACLReloadHandler(ctx, runtimeDeps, logger),
		AlarmAdvanceMinutes: buildAlarmAdvanceMinutesHandler(ctx, deps, runtimeDeps, logger),
	})

	return configsub.New(deps.Cache.GetClient(), applyFn, logger)
}

func buildScraperProxyHandler(
	deps BotConfigSubscriberDependencies,
	runtimeDeps BotConfigSubscriberRuntimeDependencies,
	scraperScheduler *scheduler.Scheduler,
	logger *slog.Logger,
) func(contractssettings.ScraperProxyPayloadV1) {
	return func(payload contractssettings.ScraperProxyPayloadV1) {
		sharedsettings.ApplyScraperProxyToggle(payload.Enabled, runtimeDeps.YouTubeService, runtimeDeps.HolodexService, scraperScheduler, logger)
		current := deps.Settings.Get()
		current.ScraperProxyEnabled = payload.Enabled
		if err := deps.Settings.Update(current); err != nil {
			logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
		}
	}
}

func buildACLReloadHandler(
	ctx context.Context,
	runtimeDeps BotConfigSubscriberRuntimeDependencies,
	logger *slog.Logger,
) func(contractssettings.ACLPayloadV1) {
	return func(payload contractssettings.ACLPayloadV1) {
		if runtimeDeps.ACL == nil {
			return
		}

		if err := runtimeDeps.ACL.Reload(ctx); err != nil {
			logger.Warn("Failed to reload ACL after config update",
				slog.String("reason", payload.Reason),
				slog.Any("error", err),
			)
			return
		}

		logger.Info("Reloaded ACL after config update",
			slog.String("reason", payload.Reason),
			slog.String("room", payload.Room),
			slog.String("mode", payload.Mode),
		)
	}
}

func buildAlarmAdvanceMinutesHandler(
	ctx context.Context,
	deps BotConfigSubscriberDependencies,
	runtimeDeps BotConfigSubscriberRuntimeDependencies,
	logger *slog.Logger,
) func(contractssettings.AlarmAdvanceMinutesPayloadV1) {
	return func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
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
	}
}

func PersistedTargetMinutes(alarmAdvanceMinutes int, targetMinutes []int) []int {
	if len(targetMinutes) > 0 {
		return sharedchecker.ResolveConfiguredTargetMinutes(targetMinutes)
	}

	return sharedchecker.BuildRuntimeTargetMinutes(alarmAdvanceMinutes)
}
