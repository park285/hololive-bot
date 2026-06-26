package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func InitScraperHolodexFoundation(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*ScraperHolodexFoundation, error) {
	holodexAPIKey := appConfig.Holodex.APIKey
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)

	scraperProxyConfig := providersScraperProxyConfig(appConfig)

	sharedRL, err := providers.ProvideYouTubeProducerRateLimiter(infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube producer rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperService(
		infra.Cache,
		memberServiceAdapter,
		scraperProxyConfig,
		sharedRL,
		logger,
	)

	holodexService, err := providers.ProvideHolodexService(
		appConfig.Holodex.BaseURL,
		holodexAPIKey,
		infra.Cache,
		scraperService,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	return &ScraperHolodexFoundation{
		HolodexService:       holodexService,
		MemberServiceAdapter: memberServiceAdapter,
		SharedRL:             sharedRL,
	}, nil
}

func InitScraperHolodexProfileFoundation(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*ScraperHolodexProfileFoundation, error) {
	foundation, err := InitScraperHolodexFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, err
	}

	profileService, err := providers.ProvideProfileService(ctx, infra.Cache, foundation.MemberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("provide profile service: %w", err)
	}

	return &ScraperHolodexProfileFoundation{
		HolodexService:       foundation.HolodexService,
		MemberServiceAdapter: foundation.MemberServiceAdapter,
		ProfileService:       profileService,
		SharedRL:             foundation.SharedRL,
	}, nil
}

func providersScraperProxyConfig(appConfig *config.Config) scraper.ProxyConfig {
	return scraper.ProxyConfig{Enabled: appConfig.Scraper.ProxyEnabled, URL: appConfig.Scraper.ProxyURL}
}
