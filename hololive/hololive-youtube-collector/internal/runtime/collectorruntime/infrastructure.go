package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/holodexcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/officialcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type collectorInfrastructure struct {
	cache        cache.Client
	postgres     database.Client
	youtubejs    *youtubejs.Helper
	youtubejsRPC *youtubejs.RPC
	holodex      *holodexcollector.Client
	official     *officialcollector.Client
	cleanup      func()
	closeOnce    sync.Once
	closeErr     error
}

func initInfrastructure(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (*collectorInfrastructure, error) {
	if appConfig == nil {
		return nil, fmt.Errorf("build collector infra: config is nil")
	}
	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, appConfig.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("build collector infra: %w", err)
	}
	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, &appConfig.Postgres, logger)
	if err != nil {
		cleanupCache()
		return nil, fmt.Errorf("build collector infra: %w", err)
	}
	cleanup := func() {
		cleanupDB()
		cleanupCache()
	}
	collector := appConfig.YouTubeCollector.OrDefault()
	helper, rpc, err := startYouTubeJSHelper(ctx, &appConfig.Scraper, &collector, ratelimiter.New(collector.RequestInterval))
	if err != nil {
		cleanup()
		return nil, err
	}
	collectorInfra := &collectorInfrastructure{
		cache:        cacheResources.Service,
		postgres:     databaseResources.Service,
		youtubejs:    helper,
		youtubejsRPC: rpc,
		cleanup:      cleanup,
	}
	maxBody := int64(collector.MaxAggregateBytes)
	if appConfig.MaxResponseBodyBytes > 0 && appConfig.MaxResponseBodyBytes < maxBody {
		maxBody = appConfig.MaxResponseBodyBytes
	}
	holodex, err := holodexcollector.NewClient(nil, appConfig.Holodex.BaseURL, appConfig.Holodex.APIKey, appConfig.Holodex.Timeout, maxBody)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("build holodex collector client: %w", err), collectorInfra.Close())
	}
	collectorInfra.holodex = holodex
	official, err := officialcollector.NewClient(nil, appConfig.OfficialSchedule.BaseURL, appConfig.OfficialSchedule.Timeout, maxBody)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("build official schedule collector client: %w", err), collectorInfra.Close())
	}
	collectorInfra.official = official
	return collectorInfra, nil
}

func (i *collectorInfrastructure) Close() error {
	if i == nil {
		return nil
	}
	i.closeOnce.Do(func() {
		var helperErr error
		if i.youtubejs != nil {
			helperErr = i.youtubejs.Close()
		}
		if i.cleanup != nil {
			i.cleanup()
		}
		i.closeErr = helperErr
	})
	return i.closeErr
}

func startYouTubeJSHelper(
	ctx context.Context,
	scraperConfig *settings.ScraperConfig,
	collector *settings.YouTubeCollectorConfig,
	limiter *ratelimiter.RateLimiter,
) (*youtubejs.Helper, *youtubejs.RPC, error) {
	if scraperConfig == nil {
		scraperConfig = &settings.ScraperConfig{}
	}
	helper, rpc, err := youtubejs.Start(ctx, &youtubejs.Config{
		ProxyURL:  scraperConfig.ProxyURL,
		ProxyOn:   scraperConfig.ProxyEnabled,
		Timeout:   collector.YouTubeJSTimeout,
		BodyLimit: int64(collector.MaxAggregateBytes),
		Limiter:   limiter,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start youtube.js helper: %w", err)
	}
	return helper, rpc, nil
}
