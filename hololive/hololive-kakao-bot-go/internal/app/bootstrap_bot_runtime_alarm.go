package app

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
)

type runtimeAlarmSchedulerBuilder func(context.Context, *config.Config, *coreInfrastructure, *slog.Logger) runtimeAlarmScheduler

func defaultRuntimeAlarmSchedulerBuilder(context.Context, *config.Config, *coreInfrastructure, *slog.Logger) runtimeAlarmScheduler {
	return nil
}

func buildRuntimeAlarmScheduler(
	ctx context.Context,
	cfg *config.Config,
	infra *coreInfrastructure,
	logger *slog.Logger,
) runtimeAlarmScheduler {
	if infra == nil || infra.runtimeAlarmSchedulerBuilder == nil {
		return nil
	}
	return infra.runtimeAlarmSchedulerBuilder(ctx, cfg, infra, logger)
}
