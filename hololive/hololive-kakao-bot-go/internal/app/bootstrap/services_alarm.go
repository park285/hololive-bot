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
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
)

func InitAlarmDependencies(
	chzzkCfg config.ChzzkConfig,
	twitchCfg config.TwitchConfig,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService cache.Client,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*AlarmDependencies, error) {
	httpClient := httputil.NewExternalAPIClient(10 * time.Second)
	chzzkClient := ProvideChzzkClient(httpClient, chzzkCfg, logger)
	twitchClient := ProvideTwitchClient(twitchCfg, logger)
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
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*AlarmModeComponents, error) {
	alarmDeps, alarmErr := InitAlarmDependencies(
		cfg.Chzzk,
		cfg.Twitch,
		cfg.Notification.AdvanceMinutes,
		cfg.Scraper.ProxyEnabled,
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
