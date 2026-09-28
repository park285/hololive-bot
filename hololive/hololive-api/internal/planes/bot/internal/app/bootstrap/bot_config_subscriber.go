package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

func BuildBotConfigSubscriber(
	ctx context.Context,
	deps BotConfigSubscriberDependencies,
	runtimeDeps BotConfigSubscriberRuntimeDependencies,
	logger *slog.Logger,
) *configsub.Subscriber {
	// scraper_proxy 갱신은 DEC-20260926-hololive-legacy-env-config-retirement로 퇴역해 처리기가 없다.
	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		ACL:                 buildACLReloadHandler(ctx, runtimeDeps, logger),
		AlarmAdvanceMinutes: buildAlarmAdvanceMinutesHandler(ctx, deps, runtimeDeps, logger),
	})

	return configsub.New(deps.Cache.GetClient(), applyFn, logger)
}

func buildACLReloadHandler(
	ctx context.Context,
	runtimeDeps BotConfigSubscriberRuntimeDependencies,
	logger *slog.Logger,
) func(contractssettings.ACLPayloadV1) {
	baseCtx := context.WithoutCancel(ctx)

	return func(payload contractssettings.ACLPayloadV1) {
		if runtimeDeps.ACL == nil {
			return
		}

		reloadCtx, cancel := context.WithTimeout(baseCtx, constants.RequestTimeout.AdminRequest)
		defer cancel()

		if err := runtimeDeps.ACL.Reload(reloadCtx); err != nil {
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
	baseCtx := context.WithoutCancel(ctx)

	return func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
		updateCtx, cancel := context.WithTimeout(baseCtx, constants.RequestTimeout.AlarmService)
		targets := runtimeDeps.AlarmCRUD.UpdateAlarmAdvanceMinutes(updateCtx, payload.Minutes)

		cancel()

		// alarm-worker client는 실패 시 빈 슬라이스를 돌려준다. 빈 결과는 설정 영속화를 건너뛴다.
		if len(targets) == 0 {
			logger.Warn("Skipped persisting alarm_advance_minutes: alarm update returned no targets",
				slog.Int("minutes", payload.Minutes),
			)

			return
		}

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
