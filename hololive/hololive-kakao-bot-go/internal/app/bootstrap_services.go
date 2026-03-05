package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

// coreInfrastructure 는 Bot 런타임 구성에 필요한 의존성/서비스 묶음을 담는다.
type coreInfrastructure struct {
	deps                         *bot.Dependencies
	alarmService                 *notification.AlarmService
	alarmCRUD                    domain.AlarmCRUD
	holodexService               *holodex.Service // 구체 타입 참조 (concrete 필요 시 사용)
	ytStack                      *providers.YouTubeStack
	photoSync                    *holodex.PhotoSyncService
	templateRenderer             *template.Renderer
	templateAdminSvc             *template.AdminService
	sharedRL                     *scraper.RateLimiter // YouTube 전역 RateLimiter
	runtimeAlarmSchedulerBuilder runtimeAlarmSchedulerBuilder
	cleanupCache                 func()
	cleanupDB                    func()
}

// initCoreInfrastructure 는 공통 인프라를 초기화한다.
//
//nolint:funlen // bootstrap wiring; keep the dependency graph visible in one place
func initCoreInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *coreInfrastructure, retErr error) {
	irisClient := providers.ProvideIrisClient(cfg.Iris, logger)

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

	templateRenderer := template.NewRenderer(infra.postgresService.GetGormDB(), logger)
	messageAdapter := adapter.NewMessageAdapter(cfg.Bot.Prefix)
	formatter := adapter.NewResponseFormatter(cfg.Bot.Prefix, templateRenderer)

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
	scraperService := providers.ProvideScraperService(infra.cacheService, memberServiceAdapter, scraperProxyConfig, sharedRL, logger)
	holodexService, err := providers.ProvideHolodexService(cfg.Holodex.BaseURL, holodexAPIKeys, infra.cacheService, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	profileService, err := providers.ProvideProfileService(ctx, infra.cacheService, memberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("provide profile service: %w", err)
	}

	alarmRepository := ProvideAlarmRepository(infra.postgresService, logger)
	alarmMode, err := initAlarmModeComponents(ctx, cfg, infra, holodexService, memberServiceAdapter, alarmRepository, logger)
	if err != nil {
		return nil, err
	}

	memberMatcher := ProvideMemberMatcher(ctx, alarmMode.memberDataSource, infra.cacheService, holodexService, logger)
	youTubeStatsRepository := providers.ProvideYouTubeStatsRepository(infra.postgresService, logger)
	youTubeStack := providers.ProvideYouTubeStack(ctx, cfg.YouTube, cfg.Scraper, infra.cacheService, holodexService, memberServiceAdapter, youTubeStatsRepository, alarmMode.alarmService, irisClient, formatter, sharedRL, logger)
	activityLogger := ProvideActivityLogger(logger)
	settingsService := providers.ProvideSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)

	aclService, err := ProvideACLService(ctx, cfg.Kakao.ACLEnabled, cfg.Kakao.Rooms, infra.postgresService, infra.cacheService, logger)
	if err != nil {
		return nil, err
	}

	majorEventRepo, memberNewsService := resolveLLMSchedulerClients(cfg, logger)

	workerPool, err := ProvideAlarmWorkerPool()
	if err != nil {
		return nil, fmt.Errorf("provide alarm worker pool: %w", err)
	}

	modules := buildBotDependencyModules(
		cfg,
		infra,
		alarmMode,
		holodexService,
		messageAdapter,
		formatter,
		irisClient,
		profileService,
		memberMatcher,
		youTubeStack,
		activityLogger,
		settingsService,
		aclService,
		majorEventRepo,
		memberNewsService,
		workerPool,
		logger,
	)
	deps := ProvideBotDependencies(modules)

	return &coreInfrastructure{
		deps:                         deps,
		alarmService:                 alarmMode.alarmService,
		alarmCRUD:                    alarmMode.alarmCRUD,
		holodexService:               holodexService,
		ytStack:                      youTubeStack,
		photoSync:                    holodex.NewPhotoSyncService(holodexService, infra.memberRepo, logger),
		templateRenderer:             templateRenderer,
		templateAdminSvc:             template.NewAdminService(repository.NewTemplateRepository(infra.postgresService.GetGormDB(), logger), templateRenderer, logger),
		sharedRL:                     sharedRL,
		runtimeAlarmSchedulerBuilder: defaultRuntimeAlarmSchedulerBuilder,
		cleanupCache:                 infra.cleanupCache,
		cleanupDB:                    infra.cleanupDB,
	}, nil
}
