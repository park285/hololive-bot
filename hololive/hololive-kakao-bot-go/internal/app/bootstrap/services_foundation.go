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
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*ScraperHolodexFoundation, error) {
	holodexAPIKey := cfg.Holodex.APIKey
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)

	scraperProxyConfig := providersScraperProxyConfig(cfg)

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
		cfg.Holodex.BaseURL,
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
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*ScraperHolodexProfileFoundation, error) {
	foundation, err := InitScraperHolodexFoundation(ctx, cfg, infra, logger)
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

func providersScraperProxyConfig(cfg *config.Config) scraper.ProxyConfig {
	return scraper.ProxyConfig{Enabled: cfg.Scraper.ProxyEnabled, URL: cfg.Scraper.ProxyURL}
}
