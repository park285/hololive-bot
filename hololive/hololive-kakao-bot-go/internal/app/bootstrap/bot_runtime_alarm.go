package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"

	alarmscheduler "github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/scheduler"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

type RuntimeAlarmScheduler interface {
	Start(ctx context.Context)
}

func NewAlarmWorkerRuntimeScheduler(
	cfg *config.Config,
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	alarmCRUD domain.AlarmCRUD,
	logger *slog.Logger,
) (RuntimeAlarmScheduler, error) {
	if cfg == nil {
		return nil, errors.New("new alarm worker runtime scheduler: config is nil")
	}
	if cacheSvc == nil {
		return nil, errors.New("new alarm worker runtime scheduler: cache is nil")
	}
	if holodexSvc == nil {
		return nil, errors.New("new alarm worker runtime scheduler: holodex service is nil")
	}
	if chzzkClient == nil {
		return nil, errors.New("new alarm worker runtime scheduler: chzzk client is nil")
	}
	if twitchClient == nil {
		return nil, errors.New("new alarm worker runtime scheduler: twitch client is nil")
	}
	if alarmCRUD == nil {
		return nil, errors.New("new alarm worker runtime scheduler: alarm CRUD is nil")
	}

	scheduler, err := alarmscheduler.NewRuntimeScheduler(
		cacheSvc,
		holodexSvc,
		chzzkClient,
		twitchClient,
		alarmCRUD,
		cfg.Notification,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("new alarm worker runtime scheduler: %w", err)
	}

	return scheduler, nil
}
