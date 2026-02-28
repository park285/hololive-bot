package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
)

// coreInfrastructure 는 공통 인프라 의존성을 담는다.
type coreInfrastructure struct {
	deps             *bot.Dependencies
	alarmService     *notification.AlarmService // 레거시 모드에서만 non-nil (HTTP 클라이언트 모드에서 nil)
	alarmCRUD        domain.AlarmCRUD           // AlarmCRUD 인터페이스 (HTTP client 또는 AlarmService)
	holodexService   *holodex.Service           // 구체 타입 참조 (concrete 필요 시 사용)
	ytStack          *providers.YouTubeStack
	photoSync        *holodex.PhotoSyncService
	templateRenderer *template.Renderer
	templateAdminSvc *template.AdminService
	sharedRL         *scraper.RateLimiter // YouTube 전역 RateLimiter
	cleanupCache     func()
	cleanupDB        func()
}

// infraResources 는 캐시/DB 리소스를 담는다.
type infraResources struct {
	cacheService    *cache.Service
	postgresService *database.PostgresService
	memberRepo      *member.Repository
	memberCache     *member.Cache
	cleanupCache    func()
	cleanupDB       func()
}

type alarmModeComponents struct {
	alarmCRUD        domain.AlarmCRUD
	alarmService     *notification.AlarmService
	chzzkClient      *chzzk.Client
	twitchClient     *twitch.Client
	memberDataSource domain.MemberDataProvider
}

// initInfraResources 는 캐시/DB 리소스를 초기화한다.
func initInfraResources(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*infraResources, error) {
	valkeyConfig := providers.ProvideValkeyConfig(cfg)
	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, valkeyConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("provide cache resources: %w", err)
	}
	cacheService := providers.ProvideCacheService(cacheResources)

	postgresConfig := providers.ProvidePostgresConfig(cfg)
	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, postgresConfig, logger)
	if err != nil {
		cleanupCache()
		return nil, fmt.Errorf("provide database resources: %w", err)
	}
	postgresService := providers.ProvidePostgresService(databaseResources)

	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		cleanupDB()
		cleanupCache()
		return nil, fmt.Errorf("provide member cache: %w", err)
	}

	return &infraResources{
		cacheService:    cacheService,
		postgresService: postgresService,
		memberRepo:      memberRepository,
		memberCache:     memberCache,
		cleanupCache:    cleanupCache,
		cleanupDB:       cleanupDB,
	}, nil
}

func initAlarmModeComponents(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	holodexService *holodex.Service,
	memberServiceAdapter *member.ServiceAdapter,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmModeComponents, error) {
	if cfg.AlarmDispatcherURL != "" {
		memberServiceAdapter2 := providers.ProvideMemberServiceAdapter(infra.memberCache, logger)
		return &alarmModeComponents{
			alarmCRUD:        alarm.NewClient(cfg.AlarmDispatcherURL, logger),
			memberDataSource: providers.ProvideMembersData(memberServiceAdapter2),
		}, nil
	}

	alarmDeps, alarmErr := initAlarmDependencies(
		cfg.Chzzk,
		cfg.Twitch,
		cfg.Notification.AdvanceMinutes,
		cfg.Scraper.ProxyEnabled,
		infra.cacheService,
		holodexService,
		memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if alarmErr != nil {
		return nil, alarmErr
	}

	if warnErr := alarmDeps.alarmService.WarmCacheFromDB(ctx); warnErr != nil {
		logger.Warn("Failed to warm alarm cache from DB", "error", warnErr)
	}

	return &alarmModeComponents{
		alarmCRUD:        alarmDeps.alarmService,
		alarmService:     alarmDeps.alarmService,
		chzzkClient:      alarmDeps.chzzkClient,
		twitchClient:     alarmDeps.twitchClient,
		memberDataSource: alarmDeps.memberDataProvider,
	}, nil
}

// initCoreInfrastructure 는 공통 인프라를 초기화한다.
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
	messageStack := providers.ProvideMessageStack(cfg.Bot.Prefix, templateRenderer)

	holodexAPIKeys := providers.ProvideHolodexAPIKeys(cfg.Holodex)
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(infra.memberCache, logger)
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: cfg.Scraper.ProxyEnabled,
		URL:     cfg.Scraper.ProxyURL,
	}
	// YouTube 전역 RateLimiter 생성 (1요청/3초 = 20요청/분)
	sharedRL := scraper.NewRateLimiter(3 * time.Second)
	if constants.YouTubeScraperDistributedRateLimitConfig.Enabled {
		distributedLimiter, distErr := ratelimit.NewSlidingWindowLimiter(
			infra.cacheService,
			constants.YouTubeScraperDistributedRateLimitConfig.KeyPrefix,
			logger,
		)
		if distErr != nil {
			return nil, fmt.Errorf("initialize scraper distributed rate limiter: %w", distErr)
		}
		if distErr := sharedRL.ConfigureDistributed(
			distributedLimiter,
			constants.YouTubeScraperDistributedRateLimitConfig.Limit,
			constants.YouTubeScraperDistributedRateLimitConfig.Window,
		); distErr != nil {
			return nil, fmt.Errorf("configure scraper distributed rate limiter: %w", distErr)
		}
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

	alarmRepository := providers.ProvideAlarmRepository(infra.postgresService, logger)
	alarmMode, err := initAlarmModeComponents(ctx, cfg, infra, holodexService, memberServiceAdapter, alarmRepository, logger)
	if err != nil {
		return nil, err
	}
	if cfg.AlarmDispatcherURL != "" {
		logger.Info("Alarm CRUD: HTTP client mode", slog.String("url", cfg.AlarmDispatcherURL))
	}

	memberMatcher := providers.ProvideMemberMatcher(ctx, alarmMode.memberDataSource, infra.cacheService, holodexService, logger)
	youTubeStatsRepository := providers.ProvideYouTubeStatsRepository(infra.postgresService, logger)
	youTubeStack := providers.ProvideYouTubeStack(ctx, cfg.YouTube, cfg.Scraper, infra.cacheService, holodexService, memberServiceAdapter, youTubeStatsRepository, alarmMode.alarmService, irisClient, messageStack.Formatter, sharedRL, logger)
	activityLogger := ProvideActivityLogger(cfg.Logging.Dir, logger)
	settingsService := providers.ProvideSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)

	aclService, err := ProvideACLService(ctx, cfg.Kakao.ACLEnabled, cfg.Kakao.Rooms, infra.postgresService, infra.cacheService, logger)
	if err != nil {
		return nil, err
	}

	majorEventRepo := providers.ProvideMajorEventRepository(ctx, infra.postgresService, logger, cfg.Postgres.AutoPrepareSchema)
	memberNewsService := initMemberNewsService(ctx, cfg.Cliproxy, cfg.LLM, cfg.Exa, infra.postgresService, infra.cacheService, alarmMode.memberDataSource, logger)

	workerPool, err := providers.ProvideAlarmWorkerPool()
	if err != nil {
		return nil, fmt.Errorf("provide alarm worker pool: %w", err)
	}

	deps := ProvideBotDependencies(cfg.Bot.SelfUser, cfg.Iris.BaseURL, cfg.Notification, logger, irisClient, messageStack, infra.cacheService, infra.postgresService, infra.memberRepo, infra.memberCache, holodexService, alarmMode.chzzkClient, alarmMode.twitchClient, profileService, alarmMode.alarmCRUD, memberMatcher, alarmMode.memberDataSource, youTubeStack, activityLogger, settingsService, aclService, majorEventRepo, memberNewsService, workerPool)

	return &coreInfrastructure{
		deps:             deps,
		alarmService:     alarmMode.alarmService,
		alarmCRUD:        alarmMode.alarmCRUD,
		holodexService:   holodexService,
		ytStack:          youTubeStack,
		photoSync:        holodex.NewPhotoSyncService(holodexService, infra.memberRepo, logger),
		templateRenderer: templateRenderer,
		templateAdminSvc: template.NewAdminService(repository.NewTemplateRepository(infra.postgresService.GetGormDB(), logger), templateRenderer, logger),
		sharedRL:         sharedRL,
		cleanupCache:     infra.cleanupCache,
		cleanupDB:        infra.cleanupDB,
	}, nil
}

// InitializeBotDependencies - 봇 의존성을 초기화합니다.
func InitializeBotDependencies(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*bot.Dependencies, func(), error) {
	infra, err := initCoreInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		infra.cleanupDB()
		infra.cleanupCache()
	}

	return infra.deps, cleanup, nil
}

// InitializeBotRuntime - cmd/bot 런타임 (Bot + MQ + Admin API 구성요소)
func InitializeBotRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*BotRuntime, func(), error) {
	infra, err := initCoreInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	runtime, err := buildBotRuntime(ctx, cfg, logger, infra)
	if err != nil {
		infra.cleanupDB()
		infra.cleanupCache()
		return nil, nil, err
	}

	cleanup := func() {
		infra.cleanupDB()
		infra.cleanupCache()
	}

	return runtime, cleanup, nil
}
