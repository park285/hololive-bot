package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"

	alarmscheduler "github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/scheduler"
)

type RuntimeAlarmScheduler interface {
	Start(ctx context.Context)
}

func BuildAlarmRuntimeScheduler(
	cfg *config.Config,
	infra *CoreInfrastructure,
	logger *slog.Logger,
) (RuntimeAlarmScheduler, error) {
	if cfg == nil {
		return nil, errors.New("build alarm runtime scheduler: config is nil")
	}
	if infra == nil {
		return nil, errors.New("build alarm runtime scheduler: infrastructure is nil")
	}
	if infra.Deps == nil {
		return nil, errors.New("build alarm runtime scheduler: bot dependencies are nil")
	}
	if infra.Deps.Cache == nil {
		return nil, errors.New("build alarm runtime scheduler: cache dependency is nil")
	}
	if infra.HolodexService == nil {
		return nil, errors.New("build alarm runtime scheduler: holodex service is nil")
	}
	if infra.Deps.Chzzk == nil {
		return nil, errors.New("build alarm runtime scheduler: chzzk client is nil")
	}
	if infra.Deps.Twitch == nil {
		return nil, errors.New("build alarm runtime scheduler: twitch client is nil")
	}
	if infra.AlarmService == nil {
		return nil, errors.New("build alarm runtime scheduler: alarm service is nil")
	}

	scheduler, err := alarmscheduler.NewRuntimeScheduler(
		infra.Deps.Cache,
		infra.HolodexService,
		infra.Deps.Chzzk,
		infra.Deps.Twitch,
		infra.AlarmService,
		cfg.Notification,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("build alarm runtime scheduler: %w", err)
	}

	return scheduler, nil
}
