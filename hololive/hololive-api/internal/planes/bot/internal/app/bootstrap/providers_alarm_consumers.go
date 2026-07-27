package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"

	"github.com/park285/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"

	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

func ProvideAlarmService(
	advanceMinutes []int,
	cacheClient cache.Client,
	holodexService *holodexprovider.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	memberData domain.MemberDataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmservice.AlarmService, error) {
	service, err := alarmservice.NewAlarmService(
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

func ProvideAlarmWorkerPool(cfg settings.WorkerPoolConfig) *workerpool.QueuedPool {
	return workerpool.NewQueued(workerpool.QueuedConfig{
		Workers:   cfg.Workers,
		QueueSize: cfg.QueueSize,
	})
}

func ProvideMatcher(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	cacheClient cache.Client,
	holodexService *holodexprovider.Service,
	logger *slog.Logger,
) *matcher.Matcher {
	return matcher.NewMatcher(ctx, membersData, cacheClient, holodexService, nil, logger)
}
