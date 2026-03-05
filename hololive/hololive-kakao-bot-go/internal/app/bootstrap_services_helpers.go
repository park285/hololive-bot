package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
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

type alarmYouTubeStackComponents struct {
	alarmMode       *alarmModeComponents
	memberMatcher   *matcher.MemberMatcher
	youTubeStack    *providers.YouTubeStack
	activityLogger  *activity.Logger
	settingsService settings.ReadWriter
}

func initAlarmYouTubeStack(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	foundation *scraperHolodexProfileFoundation,
	irisClient iris.Client,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) (*alarmYouTubeStackComponents, error) {
	alarmRepository := ProvideAlarmRepository(infra.postgresService, logger)
	alarmMode, err := initAlarmModeComponents(
		ctx,
		cfg,
		infra,
		foundation.holodexService,
		foundation.memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if err != nil {
		return nil, err
	}

	memberMatcher := ProvideMemberMatcher(
		ctx,
		alarmMode.memberDataSource,
		infra.cacheService,
		foundation.holodexService,
		logger,
	)
	youTubeStatsRepository := providers.ProvideYouTubeStatsRepository(infra.postgresService, logger)
	youTubeStack := providers.ProvideYouTubeStack(
		ctx,
		cfg.YouTube,
		cfg.Scraper,
		infra.cacheService,
		foundation.holodexService,
		foundation.memberServiceAdapter,
		youTubeStatsRepository,
		alarmMode.alarmService,
		irisClient,
		formatter,
		foundation.sharedRL,
		logger,
	)

	return &alarmYouTubeStackComponents{
		alarmMode:      alarmMode,
		memberMatcher:  memberMatcher,
		youTubeStack:   youTubeStack,
		activityLogger: ProvideActivityLogger(logger),
		settingsService: providers.ProvideSettingsService(
			cfg.Notification.AdvanceMinutes,
			cfg.Scraper.ProxyEnabled,
			logger,
		),
	}, nil
}

type coreIntegrationServices struct {
	aclService        *acl.Service
	majorEventRepo    command.MajorEventRepository
	memberNewsService command.MemberNewsService
	workerPool        *workerpool.Pool
}

func initCoreIntegrationServices(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	logger *slog.Logger,
) (*coreIntegrationServices, error) {
	aclService, err := ProvideACLService(
		ctx,
		cfg.Kakao.ACLEnabled,
		cfg.Kakao.Rooms,
		infra.postgresService,
		infra.cacheService,
		logger,
	)
	if err != nil {
		return nil, err
	}

	majorEventRepo, memberNewsService := resolveLLMSchedulerClients(cfg, logger)

	workerPool, err := ProvideAlarmWorkerPool()
	if err != nil {
		return nil, fmt.Errorf("provide alarm worker pool: %w", err)
	}

	return &coreIntegrationServices{
		aclService:        aclService,
		majorEventRepo:    majorEventRepo,
		memberNewsService: memberNewsService,
		workerPool:        workerPool,
	}, nil
}

func buildTemplateAdminService(
	infra *infraResources,
	templateRenderer *template.Renderer,
	logger *slog.Logger,
) *template.AdminService {
	return template.NewAdminService(
		repository.NewTemplateRepository(infra.postgresService.GetGormDB(), logger),
		templateRenderer,
		logger,
	)
}
