package app

import (
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"

	alarmscheduler "github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/scheduler"
)

func buildAlarmRuntimeScheduler(
	cfg *config.Config,
	infra *coreInfrastructure,
	logger *slog.Logger,
) (runtimeAlarmScheduler, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: config is nil")
	}
	if infra == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: infrastructure is nil")
	}
	if infra.deps == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: bot dependencies are nil")
	}

	if infra.deps.Cache == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: cache dependency is nil")
	}
	if infra.holodexService == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: holodex service is nil")
	}
	if infra.deps.Chzzk == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: chzzk client is nil")
	}
	if infra.deps.Twitch == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: twitch client is nil")
	}
	if infra.alarmService == nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: alarm service is nil")
	}

	scheduler, err := alarmscheduler.NewRuntimeScheduler(
		infra.deps.Cache,
		infra.holodexService,
		infra.deps.Chzzk,
		infra.deps.Twitch,
		infra.alarmService,
		cfg.Notification,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: %w", err)
	}

	return scheduler, nil
}
