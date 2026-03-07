package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// streamIngesterInfrastructure: stream-ingester 전용 인프라 (alarm/ACL/activity 미포함).
type streamIngesterInfrastructure struct {
	cacheService     cache.Client
	postgresService  database.Client
	membersData      member.DataProvider
	irisClient       iris.Client
	settingsService  settings.ReadWriter
	holodexService   *holodex.Service
	ytStack          *providers.YouTubeStack
	photoSync        *holodex.PhotoSyncService
	templateRenderer *template.Renderer
	sharedRL         *scraper.RateLimiter
	cleanupCache     func()
	cleanupDB        func()
}

// initStreamIngesterInfrastructure: stream-ingester에 필요한 최소 인프라만 초기화한다.
// alarm/ACL/activity/workerPool 등 bot 전용 구성요소를 제외한다.
func initStreamIngesterInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *streamIngesterInfrastructure, retErr error) {
	infra, err := initInfraResources(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil {
			infra.cleanupDB()
			infra.cleanupCache()
		}
	}()

	irisClient := providers.ProvideIrisClient(cfg.Iris, logger)
	templateRenderer := template.NewRenderer(infra.postgresService.GetGormDB(), logger)

	holodexAPIKey := providers.ProvideHolodexAPIKey(cfg.Holodex)
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(infra.memberCache, logger)
	membersData := memberServiceAdapter
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: cfg.Scraper.ProxyEnabled,
		URL:     cfg.Scraper.ProxyURL,
	}

	sharedRL, err := providers.ProvideYouTubeScraperRateLimiter(infra.cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube scraper rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperService(infra.cacheService, memberServiceAdapter, scraperProxyConfig, sharedRL, logger)
	holodexService, err := providers.ProvideHolodexService(cfg.Holodex.BaseURL, holodexAPIKey, infra.cacheService, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	youTubeStatsRepository := providers.ProvideYouTubeStatsRepository(infra.postgresService, logger)
	// stream-ingester는 alarm dispatch가 없으므로 alarmSvc=nil로 전달
	youTubeStack := providers.ProvideYouTubeStack(ctx, cfg.YouTube, cfg.Scraper, infra.cacheService, holodexService, memberServiceAdapter, youTubeStatsRepository, nil, irisClient, nil, sharedRL, logger)

	settingsService := providers.ProvideSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)

	return &streamIngesterInfrastructure{
		cacheService:     infra.cacheService,
		postgresService:  infra.postgresService,
		membersData:      membersData,
		irisClient:       irisClient,
		settingsService:  settingsService,
		holodexService:   holodexService,
		ytStack:          youTubeStack,
		photoSync:        holodex.NewPhotoSyncService(holodexService, infra.memberRepo, logger),
		templateRenderer: templateRenderer,
		sharedRL:         sharedRL,
		cleanupCache:     infra.cleanupCache,
		cleanupDB:        infra.cleanupDB,
	}, nil
}
