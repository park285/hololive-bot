package collectorruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/holodexcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/officialcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type collectorInfrastructure struct {
	cache        cache.Client
	postgres     database.Client
	memberCache  *member.Cache
	youtubejs    *youtubejs.Helper
	youtubejsRPC *youtubejs.RPC
	holodex      *holodexcollector.Client
	official     *officialcollector.Client
	cleanup      func()
}

func initInfrastructure(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (*collectorInfrastructure, error) {
	infra, err := sharedmodules.BuildInfraModule(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build collector infra: %w", err)
	}
	collector := appConfig.YouTubeCollector.OrDefault()
	helper, rpc, err := startYouTubeJSHelper(ctx, &appConfig.Scraper, collector, ratelimiter.New(collector.RequestInterval))
	if err != nil {
		infra.Cleanup()
		return nil, err
	}
	maxBody := int64(collector.MaxAggregateBytes)
	if appConfig.MaxResponseBodyBytes > 0 && appConfig.MaxResponseBodyBytes < maxBody {
		maxBody = appConfig.MaxResponseBodyBytes
	}
	holodex, err := holodexcollector.NewClient(nil, appConfig.Holodex.BaseURL, appConfig.Holodex.APIKey, appConfig.Holodex.Timeout, maxBody)
	if err != nil {
		_ = helper.Close()
		infra.Cleanup()
		return nil, fmt.Errorf("build holodex collector client: %w", err)
	}
	official, err := officialcollector.NewClient(nil, appConfig.OfficialSchedule.BaseURL, appConfig.OfficialSchedule.Timeout, maxBody)
	if err != nil {
		_ = helper.Close()
		infra.Cleanup()
		return nil, fmt.Errorf("build official schedule collector client: %w", err)
	}
	return &collectorInfrastructure{
		cache:        infra.Cache,
		postgres:     infra.Postgres,
		memberCache:  infra.MemberCache,
		youtubejs:    helper,
		youtubejsRPC: rpc,
		holodex:      holodex,
		official:     official,
		cleanup: func() {
			_ = helper.Close()
			infra.Cleanup()
		},
	}, nil
}

func startYouTubeJSHelper(
	ctx context.Context,
	scraperConfig *settings.ScraperConfig,
	collector settings.YouTubeCollectorConfig,
	limiter *ratelimiter.RateLimiter,
) (*youtubejs.Helper, *youtubejs.RPC, error) {
	if scraperConfig == nil {
		scraperConfig = &settings.ScraperConfig{}
	}
	helper, rpc, err := youtubejs.Start(ctx, youtubejs.Config{
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
