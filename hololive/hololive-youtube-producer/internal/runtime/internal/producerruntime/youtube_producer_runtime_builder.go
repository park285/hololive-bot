// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package producerruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
)

// youtubeProducerInfrastructure: youtube-producer 전용 인프라 (alarm/ACL/activity 미포함).
type youtubeProducerInfrastructure struct {
	cacheService     cache.Client
	postgresService  database.Client
	memberRepository *member.Repository
	settingsService  settings.ReadWriter
	holodexService   *holodex.Service
	ytStack          *sharedproviders.YouTubeStack
	photoSync        *holodex.PhotoSyncService
	templateRenderer *template.Renderer
	sharedRL         *scraper.RateLimiter
	scraperClient    *scraper.Client
	cleanup          func()
}

type youtubeProducerYouTubeResources struct {
	holodexService *holodex.Service
	ytStack        *sharedproviders.YouTubeStack
	photoSync      *holodex.PhotoSyncService
	sharedRL       *scraper.RateLimiter
	scraperClient  *scraper.Client
}

// initYouTubeProducerInfrastructure: youtube-producer에 필요한 최소 인프라만 초기화한다.
// alarm/ACL/activity/workerPool 등 bot 전용 구성요소를 제외한다.
func initYouTubeProducerInfrastructure(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (_ *youtubeProducerInfrastructure, retErr error) {
	infra, err := initProducerInfra(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil {
			infra.Cleanup()
		}
	}()

	templateRenderer := template.NewRenderer(infra.Postgres.GetGormDB(), logger)

	youTube, err := buildYouTubeProducerResources(ctx, appConfig, logger, infra)
	if err != nil {
		return nil, err
	}

	return &youtubeProducerInfrastructure{
		cacheService:     infra.Cache,
		postgresService:  infra.Postgres,
		memberRepository: infra.MemberRepository,
		settingsService:  sharedmodules.BuildSettingsService(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger),
		holodexService:   youTube.holodexService,
		ytStack:          youTube.ytStack,
		photoSync:        youTube.photoSync,
		templateRenderer: templateRenderer,
		sharedRL:         youTube.sharedRL,
		scraperClient:    youTube.scraperClient,
		cleanup:          infra.Cleanup,
	}, nil
}

func buildYouTubeProducerResources(ctx context.Context, appConfig *config.Config, logger *slog.Logger, infra *sharedmodules.InfraModule) (*youtubeProducerYouTubeResources, error) {
	memberServiceAdapter := sharedproviders.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
	sharedRL, err := sharedproviders.ProvideYouTubeProducerRateLimiter(infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube producer rate limiter: %w", err)
	}

	scraperClient := polling.BuildSharedClient(appConfig.Scraper, infra.Cache, sharedRL)
	scraperService := sharedproviders.ProvideScraperServiceWithYouTubeProducer(infra.Cache, memberServiceAdapter, scraperClient, logger)
	holodexService, err := sharedproviders.ProvideHolodexService(appConfig.Holodex.BaseURL, appConfig.Holodex.APIKey, infra.Cache, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	youTubeStack := sharedmodules.BuildYouTubeStack(ctx, sharedmodules.YouTubeStackParams{
		YouTubeConfig:   appConfig.YouTube,
		ScraperConfig:   appConfig.Scraper,
		CacheService:    infra.Cache,
		HolodexService:  holodexService,
		Members:         memberServiceAdapter,
		StatsRepository: ytstats.NewYouTubeStatsRepository(infra.Postgres, logger),
		AlarmState:      nil,
		Formatter:       nil,
		SharedRateLimit: sharedRL,
		Logger:          logger,
	})

	return &youtubeProducerYouTubeResources{
		holodexService: holodexService,
		ytStack:        youTubeStack,
		photoSync:      holodex.NewPhotoSyncService(holodexService, infra.MemberRepository, logger),
		sharedRL:       sharedRL,
		scraperClient:  scraperClient,
	}, nil
}
