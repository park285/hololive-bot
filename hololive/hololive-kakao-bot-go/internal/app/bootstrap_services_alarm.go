package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func initAlarmDependencies(
	chzzkCfg config.ChzzkConfig,
	twitchCfg config.TwitchConfig,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService cache.Client,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmDependencies, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	chzzkClient := ProvideChzzkClient(httpClient, chzzkCfg, logger)
	twitchClient := ProvideTwitchClient(twitchCfg, logger)
	memberDataProvider := memberServiceAdapter

	resolved := providers.ResolveAlarmAdvanceMinutes(advanceMinutes, scraperProxyEnabled, logger)
	alarmService, err := ProvideAlarmService(resolved, cacheService, holodexService, chzzkClient, twitchClient, memberDataProvider, alarmRepository, logger)
	if err != nil {
		return nil, fmt.Errorf("provide alarm service: %w", err)
	}

	return &alarmDependencies{
		alarmService:       alarmService,
		memberDataProvider: memberDataProvider,
		chzzkClient:        chzzkClient,
		twitchClient:       twitchClient,
	}, nil
}

func initAlarmModeComponents(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmModeComponents, error) {
	alarmDeps, alarmErr := initAlarmDependencies(
		cfg.Chzzk,
		cfg.Twitch,
		cfg.Notification.AdvanceMinutes,
		cfg.Scraper.ProxyEnabled,
		infra.cacheService,
		holodexService,
		memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if alarmErr != nil {
		return nil, alarmErr
	}

	if warnErr := alarmDeps.alarmService.WarmCacheFromDB(ctx); warnErr != nil {
		logger.Warn("Failed to warm alarm cache from DB", "error", warnErr)
	}

	return &alarmModeComponents{
		alarmCRUD:        alarmDeps.alarmService,
		alarmService:     alarmDeps.alarmService,
		chzzkClient:      alarmDeps.chzzkClient,
		twitchClient:     alarmDeps.twitchClient,
		memberDataSource: alarmDeps.memberDataProvider,
	}, nil
}
