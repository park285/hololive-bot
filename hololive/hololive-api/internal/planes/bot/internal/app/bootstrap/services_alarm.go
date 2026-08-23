package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"

	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/v2/pkg/httputil"
)

func InitAlarmDependencies(
	chzzkConfig settings.ChzzkConfig,
	twitchConfig *settings.TwitchConfig,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService cache.Client,
	holodexService *holodexprovider.Service,
	memberServiceAdapter domain.MemberDataProvider, alarmRepository *alarm.Repository,
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
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	holodexService *holodexprovider.Service,
	memberServiceAdapter domain.MemberDataProvider, alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*AlarmModeComponents, error) {
	if providerURL := strings.TrimSpace(appConfig.AlarmServiceURL); providerURL != "" {
		alarmClient, err := alarm.NewClientWithAPIKeyStrict(providerURL, appConfig.Server.APIKey, logger)
		if err != nil {
			return nil, fmt.Errorf("configure alarm worker client: %w", err)
		}
		httpClient := httputil.NewExternalAPIClient(10 * time.Second)
		return &AlarmModeComponents{
			AlarmCRUD:        alarmClient,
			ChzzkClient:      ProvideChzzkClient(httpClient, appConfig.Chzzk, logger),
			TwitchClient:     ProvideTwitchClient(&appConfig.Twitch, logger),
			MemberDataSource: memberServiceAdapter,
		}, nil
	}

	alarmDeps, alarmErr := InitAlarmDependencies(
		appConfig.Chzzk,
		&appConfig.Twitch,
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
