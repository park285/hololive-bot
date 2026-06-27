package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/park285/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

func ProvideAlarmService(
	advanceMinutes []int,
	cacheClient cache.Client,
	holodexService *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	memberData domain.MemberDataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*notification.AlarmService, error) {
	service, err := notification.NewAlarmService(
		cacheClient,
		holodexService,
		chzzkClient,
		twitchClient,
		memberData,
		alarmRepository,
		logger,
		advanceMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create alarm service: %w", err)
	}

	return service, nil
}

func ProvideAlarmRepository(postgres database.Client, logger *slog.Logger) *alarm.Repository {
	return alarm.NewRepository(postgres, logger)
}

func ProvideAlarmWorkerPool(cfg config.WorkerPoolConfig) *workerpool.QueuedPool {
	return workerpool.NewQueued(workerpool.QueuedConfig{
		Workers:   cfg.Workers,
		QueueSize: cfg.QueueSize,
	})
}

func ProvideMatcher(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	cacheClient cache.Client,
	holodexService *holodex.Service,
	logger *slog.Logger,
) *matcher.Matcher {
	return matcher.NewMatcher(ctx, membersData, cacheClient, holodexService, nil, logger)
}
