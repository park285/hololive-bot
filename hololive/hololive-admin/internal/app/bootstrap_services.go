package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-admin/internal/server"
	"github.com/kapu/hololive-admin/internal/service/acl"
	"github.com/kapu/hololive-admin/internal/service/activity"
	authsvc "github.com/kapu/hololive-admin/internal/service/auth"
	"github.com/kapu/hololive-admin/internal/service/configpub"
	"github.com/kapu/hololive-admin/internal/service/system"
	"github.com/kapu/hololive-admin/internal/service/trigger"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/repository"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// buildAdminComponents: admin-api 컴포넌트를 조립합니다.
func buildAdminComponents(
	ctx context.Context,
	cfg *config.AdminAPIConfig,
	logger *slog.Logger,
	cacheService cache.Client,
	postgresService database.Client,
	cleanup func(),
) (*AdminAPIRuntime, error) {
	// 멤버 서비스 초기화
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("init member cache: %w", err)
	}
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(memberCache, logger)

	holodexService, err := buildAdminHolodexService(cfg, cacheService, memberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("init holodex service: %w", err)
	}

	// YouTube StatsRepository (통계 조회용)
	statsRepo := providers.ProvideYouTubeStatsRepository(postgresService, logger)

	// 프로필 서비스
	profileService, err := providers.ProvideProfileService(ctx, cacheService, memberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("init profile service: %w", err)
	}

	// Activity 로거, Settings 서비스, ACL 서비스
	activityLogger := ProvideActivityLogger(cfg.Logging.Dir, logger)
	settingsService := providers.ProvideSettingsService([]int{5}, false, logger)

	aclService, err := ProvideACLService(ctx, true, nil, postgresService, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("init acl service: %w", err)
	}

	// 템플릿 서비스
	templateRenderer := template.NewRenderer(postgresService.GetGormDB(), logger)
	templateAdminSvc := template.NewAdminService(repository.NewTemplateRepository(postgresService.GetGormDB(), logger), templateRenderer, logger)

	// AlarmCRUD: alarm-dispatcher HTTP 클라이언트
	if strings.TrimSpace(cfg.AlarmDispatcherURL) == "" {
		return nil, fmt.Errorf("ALARM_DISPATCHER_URL is required")
	}
	alarmCRUD := alarm.NewClientWithAPIKey(cfg.AlarmDispatcherURL, cfg.Server.APIKey, logger)

	// Auth 서비스
	authService, err := ProvideAuthService(ctx, cfg.Postgres.AutoPrepareSchema, postgresService, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("init auth service: %w", err)
	}
	authHandler := ProvideAuthHandler(authService, logger)

	// ConfigPub Publisher
	publisher := configpub.New(cacheService.GetClient(), logger)

	// Pub/Sub 기반 SettingsApplier
	settingsApplier := server.NewPubSubSettingsApplier(publisher, alarmCRUD, logger)

	if strings.TrimSpace(cfg.LLMSchedulerURL) == "" {
		return nil, fmt.Errorf("LLM_SCHEDULER_INTERNAL_URL is required")
	}

	// Trigger proxy 클라이언트 (llm-scheduler 프록시)
	triggerClient := trigger.NewClient(cfg.LLMSchedulerURL, cfg.Server.APIKey, logger)

	// 시스템 수집기
	systemCollector := ProvideSystemCollector(cfg.Services, cfg.Telemetry)

	// API 도메인 핸들러 (youtube=nil, scraperProxyToggler=nil, scraperScheduler=nil)
	domainHandlers := ProvideDomainAPIHandlers(
		memberRepository,
		memberCache,
		cacheService,
		profileService,
		alarmCRUD,
		holodexService,
		nil, // youtube: admin-api에서 미사용
		nil, // scraperScheduler: admin-api에서 미사용
		statsRepo,
		activityLogger,
		settingsService,
		settingsApplier,
		aclService,
		systemCollector,
		templateAdminSvc,
		triggerClient, // MajorEventScheduler
		triggerClient, // MajorEventMonthlyScheduler
		logger,
	)

	// API 라우터 구성 (admin-api 전용)
	httpServer, err := buildAdminHTTPServer(ctx, cfg, logger, domainHandlers, authHandler)
	if err != nil {
		return nil, fmt.Errorf("build admin http server: %w", err)
	}

	return &AdminAPIRuntime{
		Config:     cfg,
		Logger:     logger,
		cleanup:    cleanup,
		httpServer: httpServer,
	}, nil
}

func buildAdminHolodexService(
	cfg *config.AdminAPIConfig,
	cacheService cache.Client,
	memberServiceAdapter member.DataProvider,
	logger *slog.Logger,
) (*holodex.Service, error) {
	sharedRL, err := buildAdminScraperRateLimiter(cacheService, logger)
	if err != nil {
		return nil, err
	}

	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: false, // admin-api에서는 프록시 미사용
	}
	holodexAPIKeys := providers.ProvideHolodexAPIKeys(cfg.Holodex)
	scraperService := providers.ProvideScraperService(cacheService, memberServiceAdapter, scraperProxyConfig, sharedRL, logger)

	holodexService, err := providers.ProvideHolodexService(cfg.Holodex.BaseURL, holodexAPIKeys, cacheService, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin holodex service: %w", err)
	}

	return holodexService, nil
}

func buildAdminScraperRateLimiter(cacheService cache.Client, logger *slog.Logger) (*scraper.RateLimiter, error) {
	sharedRL := scraper.NewRateLimiter(3 * time.Second)
	if !constants.YouTubeScraperDistributedRateLimitConfig.Enabled {
		return sharedRL, nil
	}

	distributedLimiter, err := ratelimit.NewSlidingWindowLimiter(
		cacheService,
		constants.YouTubeScraperDistributedRateLimitConfig.KeyPrefix,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("init scraper distributed rate limiter: %w", err)
	}
	if err := sharedRL.ConfigureDistributed(
		distributedLimiter,
		constants.YouTubeScraperDistributedRateLimitConfig.Limit,
		constants.YouTubeScraperDistributedRateLimitConfig.Window,
	); err != nil {
		return nil, fmt.Errorf("configure scraper distributed rate limiter: %w", err)
	}

	return sharedRL, nil
}

// ProvideSystemCollector: 시스템 리소스 수집기를 생성하여 제공합니다.
func ProvideSystemCollector(cfg config.ServicesConfig, telemetry config.TelemetryConfig) *system.Collector {
	endpoints := []system.ServiceEndpoint{
		{Name: "llm-server", URL: cfg.LLMServerHealthURL},
		{Name: "twentyq", URL: cfg.GameBotTwentyQHealthURL},
		{Name: "turtlesoup", URL: cfg.GameBotTurtleHealthURL},
	}
	return system.NewCollector(endpoints, telemetry.Enabled)
}

// ProvideDomainAPIHandlers: Hololive API 도메인 핸들러 묶음을 생성하여 제공합니다.
func ProvideDomainAPIHandlers(
	repo *member.Repository,
	memberCache *member.Cache,
	valkeyCache cache.Client,
	profilesSvc *member.ProfileService,
	alarmCRUD domain.AlarmCRUD,
	holodexSvc *holodex.Service,
	youtubeSvc youtube.Service,
	scraperScheduler *poller.Scheduler,
	statsRepo youtube.StatsDashboardRepository,
	activityLogger *activity.Logger,
	settingsSvc settings.ReadWriter,
	settingsApplier sharedserver.SettingsApplier,
	aclSvc *acl.Service,
	systemSvc *system.Collector,
	templateAdmin *template.AdminService,
	majorEventScheduler server.MajorEventScheduler,
	majorEventMonthlyScheduler server.MajorEventMonthlyScheduler,
	logger *slog.Logger,
) *server.DomainAPIHandlers {
	return server.NewAPIHandler(
		repo,
		memberCache,
		valkeyCache,
		profilesSvc,
		alarmCRUD,
		holodexSvc,
		youtubeSvc,
		scraperScheduler,
		statsRepo,
		activityLogger,
		settingsSvc,
		settingsApplier,
		aclSvc,
		systemSvc,
		templateAdmin,
		majorEventScheduler,
		majorEventMonthlyScheduler,
		logger,
	).DomainHandlers()
}

// ProvideAuthService: 세션 기반 인증 서비스를 생성하여 제공합니다.
func ProvideAuthService(
	ctx context.Context,
	autoPrepareSchema bool,
	postgres database.Client,
	cacheSvc cache.Client,
	logger *slog.Logger,
) (*authsvc.Service, error) {
	authCfg := authsvc.DefaultConfig()
	authCfg.AutoPrepareSchema = autoPrepareSchema
	svc, err := authsvc.NewService(ctx, postgres.GetGormDB(), cacheSvc, logger, authCfg)
	if err != nil {
		return nil, fmt.Errorf("create auth service: %w", err)
	}
	return svc, nil
}

// ProvideAuthHandler: /api/auth 핸들러를 생성하여 제공합니다.
func ProvideAuthHandler(authService *authsvc.Service, logger *slog.Logger) *server.AuthHandler {
	return server.NewAuthHandler(authService, logger)
}

// ProvideACLService: 접근 제어 서비스 생성 (PostgreSQL 영구화)
func ProvideACLService(
	ctx context.Context,
	kakaoACLEnabled bool,
	kakaoRooms []string,
	postgres database.Client,
	cacheSvc cache.Client,
	logger *slog.Logger,
) (*acl.Service, error) {
	svc, err := acl.NewACLService(
		ctx,
		postgres,
		cacheSvc,
		logger,
		kakaoACLEnabled,
		kakaoRooms,
	)
	if err != nil {
		return nil, fmt.Errorf("create ACL service: %w", err)
	}
	return svc, nil
}

// ProvideActivityLogger: 활동 로거 생성
func ProvideActivityLogger(logDir string, logger *slog.Logger) *activity.Logger {
	if logDir == "" {
		return activity.NewActivityLogger("", logger)
	}
	return activity.NewActivityLogger(logDir+"/activity.log", logger)
}
