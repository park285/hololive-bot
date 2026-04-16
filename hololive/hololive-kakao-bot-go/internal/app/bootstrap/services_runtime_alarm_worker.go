package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
)

func InitAlarmWorkerInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *AlarmWorkerInfrastructure, retErr error) {
	infra, err := InitInfraResources(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	defer func() {
		if retErr != nil {
			infra.Cleanup()
		}
	}()

	foundation, err := InitScraperHolodexFoundation(ctx, cfg, infra, logger)
	if err != nil {
		return nil, err
	}

	alarmRepo := ProvideAlarmRepository(infra.Postgres, logger)
	alarmMode, err := InitAlarmModeComponents(
		ctx,
		cfg,
		infra,
		foundation.HolodexService,
		foundation.MemberServiceAdapter,
		alarmRepo,
		logger,
	)
	if err != nil {
		return nil, err
	}

	return &AlarmWorkerInfrastructure{
		Cache:          infra.Cache,
		HolodexService: foundation.HolodexService,
		ChzzkClient:    alarmMode.ChzzkClient,
		TwitchClient:   alarmMode.TwitchClient,
		AlarmCRUD:      alarmMode.AlarmCRUD,
		Cleanup:        infra.Cleanup,
	}, nil
}
