package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

func InitAlarmDependencies(
	settingsFilePath string,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService cache.Client,
	holodexService *holodexprovider.Service,
	memberServiceAdapter domain.MemberDataProvider, alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*AlarmDependencies, error) {
	memberDataProvider := memberServiceAdapter

	resolved := sharedmodules.ResolvePersistedTargetMinutes(settingsFilePath, advanceMinutes, scraperProxyEnabled, logger)

	alarmService, err := ProvideAlarmService(resolved, cacheService, holodexService, memberDataProvider, alarmRepository, logger)
	if err != nil {
		return nil, fmt.Errorf("provide alarm service: %w", err)
	}

	return &AlarmDependencies{
		AlarmService:       alarmService,
		MemberDataProvider: memberDataProvider,
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

		return &AlarmModeComponents{
			AlarmCRUD:        alarmClient,
			MemberDataSource: memberServiceAdapter,
		}, nil
	}

	alarmDeps, alarmErr := InitAlarmDependencies(
		appConfig.SettingsFilePath,
		appConfig.Notification.AdvanceMinutes,
		appConfig.Scraper.ProxyEnabled,
		infra.Cache,
		holodexService,
		memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if alarmErr != nil {
		return nil, fmt.Errorf("init alarm dependencies: %w", alarmErr)
	}

	if warnErr := alarmDeps.AlarmService.WarmCacheFromDB(ctx); warnErr != nil {
		logger.Warn("Failed to warm alarm cache from DB", "error", warnErr)
	}

	return &AlarmModeComponents{
		AlarmCRUD:        alarmDeps.AlarmService,
		AlarmService:     alarmDeps.AlarmService,
		MemberDataSource: alarmDeps.MemberDataProvider,
	}, nil
}
