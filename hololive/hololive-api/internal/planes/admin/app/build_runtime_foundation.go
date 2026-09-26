package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
)

func buildScraperHolodexFoundation(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*scraperHolodexFoundation, error) {
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)

	sharedRL, err := providers.ProvideYouTubeRateLimiter(infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube producer rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperServiceWithOfficialSchedule(
		infra.Cache,
		memberServiceAdapter,
		scraper.ProxyConfig{Enabled: appConfig.Scraper.ProxyEnabled, URL: appConfig.Scraper.ProxyURL},
		sharedRL,
		logger,
		appConfig.OfficialScheduleRuntime(),
	)

	holodexService, err := providers.ProvideHolodexService(
		appConfig.Holodex.BaseURL,
		appConfig.Holodex.APIKey,
		infra.Cache,
		scraperService,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	return &scraperHolodexFoundation{
		HolodexService:       holodexService,
		MemberServiceAdapter: memberServiceAdapter,
		SharedRL:             sharedRL,
	}, nil
}

func buildAlarmModeComponents(
	ctx context.Context,
	appConfig *settings.Config,
	cacheClient cache.Client,
	holodexService *holodexprovider.Service,
	memberData domain.MemberDataProvider, alarmRepository *sharedalarm.Repository,
	logger *slog.Logger,
) (*alarmModeComponents, error) {
	if providerURL := strings.TrimSpace(appConfig.AlarmServiceURL); providerURL != "" {
		alarmClient, err := sharedalarm.NewClientWithAPIKeyStrict(providerURL, appConfig.Server.APIKey, logger)
		if err != nil {
			return nil, fmt.Errorf("configure alarm worker client: %w", err)
		}

		return &alarmModeComponents{
			AlarmCRUD:        alarmClient,
			MemberDataSource: memberData,
		}, nil
	}

	resolved := sharedmodules.ResolvePersistedTargetMinutes(appConfig.SettingsFilePath, appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger)

	alarmService, err := alarmservice.NewAlarmService(cacheClient, holodexService, memberData, alarmRepository, logger, resolved)
	if err != nil {
		return nil, fmt.Errorf("create alarm service: %w", err)
	}

	if err := alarmService.WarmCacheFromDB(ctx); err != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", err))
	}

	return &alarmModeComponents{
		AlarmCRUD:        alarmService,
		AlarmService:     alarmService,
		MemberDataSource: memberData,
	}, nil
}
