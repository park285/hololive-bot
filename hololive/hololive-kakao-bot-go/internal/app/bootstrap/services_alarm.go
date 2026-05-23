package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/park285/hololive-bot/shared-go/pkg/httputil"
)

func InitAlarmDependencies(
	chzzkConfig config.ChzzkConfig,
	twitchConfig config.TwitchConfig,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService cache.Client,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*AlarmDependencies, error) {
	httpClient := httputil.NewExternalAPIClient(10 * time.Second)
	chzzkClient := ProvideChzzkClient(httpClient, chzzkConfig, logger)
	twitchClient := ProvideTwitchClient(twitchConfig, logger)
	memberDataProvider := memberServiceAdapter

	resolved := sharedmodules.ResolvePersistedTargetMinutes(advanceMinutes, scraperProxyEnabled, logger)

	alarmService, err := ProvideAlarmService(resolved, cacheService, holodexService, chzzkClient, twitchClient, memberDataProvider, alarmRepository, logger)
	if err != nil {
		return nil, fmt.Errorf("provide alarm service: %w", err)
	}

	return &AlarmDependencies{
		AlarmService:       alarmService,
		MemberDataProvider: memberDataProvider,
		ChzzkClient:        chzzkClient,
		TwitchClient:       twitchClient,
	}, nil
}

func InitAlarmModeComponents(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*AlarmModeComponents, error) {
	alarmDeps, alarmErr := InitAlarmDependencies(
		appConfig.Chzzk,
		appConfig.Twitch,
		appConfig.Notification.AdvanceMinutes,
		appConfig.Scraper.ProxyEnabled,
		infra.Cache,
		holodexService,
		memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if alarmErr != nil {
		return nil, alarmErr
	}

	if warnErr := alarmDeps.AlarmService.WarmCacheFromDB(ctx); warnErr != nil {
		logger.Warn("Failed to warm alarm cache from DB", "error", warnErr)
	}

	return &AlarmModeComponents{
		AlarmCRUD:        alarmDeps.AlarmService,
		AlarmService:     alarmDeps.AlarmService,
		ChzzkClient:      alarmDeps.ChzzkClient,
		TwitchClient:     alarmDeps.TwitchClient,
		MemberDataSource: alarmDeps.MemberDataProvider,
	}, nil
}
