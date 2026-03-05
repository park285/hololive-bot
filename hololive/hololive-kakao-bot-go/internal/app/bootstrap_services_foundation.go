package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type scraperHolodexProfileFoundation struct {
	holodexService       *holodex.Service
	memberServiceAdapter member.DataProvider
	profileService       *member.ProfileService
	sharedRL             *scraper.RateLimiter
}

func initScraperHolodexProfileFoundation(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	logger *slog.Logger,
) (*scraperHolodexProfileFoundation, error) {
	holodexAPIKeys := providers.ProvideHolodexAPIKeys(cfg.Holodex)
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(infra.memberCache, logger)

	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: cfg.Scraper.ProxyEnabled,
		URL:     cfg.Scraper.ProxyURL,
	}
	sharedRL, err := providers.ProvideYouTubeScraperRateLimiter(infra.cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube scraper rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperService(
		infra.cacheService,
		memberServiceAdapter,
		scraperProxyConfig,
		sharedRL,
		logger,
	)
	holodexService, err := providers.ProvideHolodexService(
		cfg.Holodex.BaseURL,
		holodexAPIKeys,
		infra.cacheService,
		scraperService,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	profileService, err := providers.ProvideProfileService(ctx, infra.cacheService, memberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("provide profile service: %w", err)
	}

	return &scraperHolodexProfileFoundation{
		holodexService:       holodexService,
		memberServiceAdapter: memberServiceAdapter,
		profileService:       profileService,
		sharedRL:             sharedRL,
	}, nil
}
