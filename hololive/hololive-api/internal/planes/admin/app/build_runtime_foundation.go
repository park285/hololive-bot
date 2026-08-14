package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"

	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
)

func buildScraperHolodexProfileFoundation(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*scraperHolodexProfileFoundation, error) {
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
	sharedRL, err := providers.ProvideYouTubeProducerRateLimiter(infra.Cache, logger)
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

	profileService, err := providers.ProvideProfileService(ctx, infra.Cache, memberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("provide profile service: %w", err)
	}

	return &scraperHolodexProfileFoundation{
		HolodexService:       holodexService,
		MemberServiceAdapter: memberServiceAdapter,
		ProfileService:       profileService,
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
	chzzkClient := chzzk.NewClient(nil, "", logger)
	if strings.TrimSpace(appConfig.Chzzk.ClientID) != "" || strings.TrimSpace(appConfig.Chzzk.ClientSecret) != "" {
		chzzkClient = chzzk.NewClientWithConfig(&chzzk.ClientConfig{
			HTTPClient:   nil,
			ClientID:     appConfig.Chzzk.ClientID,
			ClientSecret: appConfig.Chzzk.ClientSecret,
			Logger:       logger,
		})
	}
	twitchClient := twitch.NewClient(&twitch.ClientConfig{
		HTTPClient:   nil,
		ClientID:     appConfig.Twitch.ClientID,
		ClientSecret: appConfig.Twitch.ClientSecret,
	}, logger)

	if providerURL := strings.TrimSpace(appConfig.AlarmServiceURL); providerURL != "" {
		alarmClient, err := sharedalarm.NewClientWithAPIKeyStrict(providerURL, appConfig.Server.APIKey, logger)
		if err != nil {
			return nil, fmt.Errorf("configure alarm worker client: %w", err)
		}
		return &alarmModeComponents{
			AlarmCRUD:        alarmClient,
			ChzzkClient:      chzzkClient,
			TwitchClient:     twitchClient,
			MemberDataSource: memberData,
		}, nil
	}

	resolved := sharedmodules.ResolvePersistedTargetMinutes(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger)
	alarmService, err := alarmservice.NewAlarmService(cacheClient, holodexService, chzzkClient, twitchClient, memberData, alarmRepository, logger, resolved)
	if err != nil {
		return nil, fmt.Errorf("create alarm service: %w", err)
	}
	if err := alarmService.WarmCacheFromDB(ctx); err != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", err))
	}

	return &alarmModeComponents{
		AlarmCRUD:        alarmService,
		AlarmService:     alarmService,
		ChzzkClient:      chzzkClient,
		TwitchClient:     twitchClient,
		MemberDataSource: memberData,
	}, nil
}
