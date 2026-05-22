package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/repository"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"

	apphttp "github.com/kapu/hololive-admin-api/internal/app/http"
	"github.com/kapu/hololive-admin-api/internal/server"
	"github.com/kapu/hololive-admin-api/internal/service/system"
	triggerclient "github.com/kapu/hololive-admin-api/internal/service/trigger"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

type scraperHolodexProfileFoundation struct {
	HolodexService       *holodex.Service
	MemberServiceAdapter member.DataProvider
	ProfileService       *member.ProfileService
	SharedRL             *scraper.RateLimiter
}

type alarmModeComponents struct {
	AlarmCRUD        domain.AlarmCRUD
	AlarmService     *notification.AlarmService
	ChzzkClient      *chzzk.Client
	TwitchClient     *twitch.Client
	MemberDataSource member.DataProvider
}

func BuildAdminAPIRuntime(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*AdminAPIRuntime, error) {
	ctx, err := normalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: build infra module: %w", err)
	}

	foundation, err := buildScraperHolodexProfileFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "foundation", err)
	}

	alarmRepository := sharedalarm.NewRepository(infra.Postgres, logger)
	alarmMode, err := buildAlarmModeComponents(ctx, appConfig, infra.Cache, foundation.HolodexService, foundation.MemberServiceAdapter, alarmRepository, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "alarm mode", err)
	}

	aclService, err := buildAdminAPIACLService(ctx, appConfig, infra, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "acl service", err)
	}

	ytStack := buildAdminAPIYouTubeStack(ctx, appConfig, infra, foundation, logger)
	templateAdmin := buildAdminAPITemplateAdmin(infra, logger)
	authService, err := buildAdminAPIAuthService(ctx, appConfig, infra, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "auth service", err)
	}

	settingsApplier, majorEventTriggerClient := buildAdminAPISettingsApplier(appConfig, foundation, alarmMode, ytStack, logger)
	systemCollector := buildAdminAPISystemCollector(appConfig)
	communityShortsOpsRepository := buildAdminAPICommunityShortsOpsRepository(infra)
	handler := buildAdminHandler(
		appConfig,
		infra,
		foundation,
		alarmMode,
		aclService,
		ytStack,
		communityShortsOpsRepository,
		settingsApplier,
		systemCollector,
		templateAdmin,
		majorEventTriggerClient,
		logger,
	)
	router, err := buildAdminAPIRouter(ctx, appConfig, infra, authService, handler, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "provide api router", err)
	}

	registerAdminAPIInternalAlarmRoutes(router, appConfig, alarmMode, logger)

	return newAdminAPIRuntime(appConfig, logger, fmt.Sprintf(":%d", appConfig.Server.Port), router, infra.Cleanup), nil
}

func cleanupAdminAPIRuntimeBuild(infra *sharedmodules.InfraModule, stage string, err error) (*AdminAPIRuntime, error) {
	infra.Cleanup()
	return nil, fmt.Errorf("build admin api runtime: %s: %w", stage, err)
}

func newAdminAPIRuntime(
	appConfig *config.Config,
	logger *slog.Logger,
	addr string,
	router *gin.Engine,
	cleanup func(),
) *AdminAPIRuntime {
	return &AdminAPIRuntime{
		Config:     appConfig,
		Logger:     logger,
		ServerAddr: addr,
		HttpServer: sharedserver.NewH2CServer(addr, router, "hololive-admin-api.http"),
		Managed:    lifecycle.NewManaged(cleanup),
	}
}

func buildAdminAPIACLService(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*acl.Service, error) {
	return acl.NewACLService(
		ctx,
		infra.Postgres,
		infra.Cache,
		logger,
		appConfig.Kakao.ACLEnabled,
		acl.ParseACLMode(appConfig.Kakao.ACLMode),
		appConfig.Kakao.Rooms,
	)
}

func buildAdminAPIAuthService(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*authsvc.Service, error) {
	authConfig := authsvc.DefaultConfig()
	authConfig.AutoPrepareSchema = appConfig.Postgres.AutoPrepareSchema
	return authsvc.NewService(ctx, infra.Postgres.GetGormDB(), infra.Cache, logger, authConfig)
}

func buildAdminAPIYouTubeStack(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	foundation *scraperHolodexProfileFoundation,
	logger *slog.Logger,
) *providers.YouTubeStack {
	statsRepository := ytstats.NewYouTubeStatsRepository(infra.Postgres, logger)
	return sharedmodules.BuildYouTubeAPIStack(ctx, sharedmodules.YouTubeAPIStackParams{
		YouTubeConfig:   appConfig.YouTube,
		ScraperConfig:   appConfig.Scraper,
		CacheService:    infra.Cache,
		StatsRepository: statsRepository,
		SharedRateLimit: foundation.SharedRL,
		Logger:          logger,
	})
}

func buildAdminAPISettingsApplier(
	appConfig *config.Config,
	foundation *scraperHolodexProfileFoundation,
	alarmMode *alarmModeComponents,
	ytStack *providers.YouTubeStack,
	logger *slog.Logger,
) (sharedsettings.SettingsApplier, *triggerclient.Client) {
	localSettingsApplier := sharedsettings.NewLocalSettingsApplier(
		ytStack.GetService(),
		foundation.HolodexService,
		nil,
		alarmMode.AlarmCRUD,
	)
	settingsApplier := newBotSettingsApplier(localSettingsApplier, nil, logger)

	if strings.TrimSpace(appConfig.LLMSchedulerURL) == "" {
		logger.Warn("LLM scheduler URL not configured; trigger routes and membernews run-now are disabled", slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"))
		return settingsApplier, nil
	}

	majorEventTriggerClient := triggerclient.NewClient(appConfig.LLMSchedulerURL, appConfig.Server.APIKey, logger)
	return newBotSettingsApplier(localSettingsApplier, majorEventTriggerClient, logger), majorEventTriggerClient
}

func buildAdminHandler(
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	foundation *scraperHolodexProfileFoundation,
	alarmMode *alarmModeComponents,
	aclService *acl.Service,
	ytStack *providers.YouTubeStack,
	communityShortsOpsRepository server.YouTubeCommunityShortsOpsRepository,
	settingsApplier sharedsettings.SettingsApplier,
	systemCollector *system.Collector,
	templateAdmin *template.AdminService,
	majorEventTriggerClient *triggerclient.Client,
	logger *slog.Logger,
) *server.Handler {
	return server.NewHandler(
		infra.MemberRepository,
		infra.MemberCache,
		infra.Cache,
		foundation.ProfileService,
		alarmMode.AlarmCRUD,
		foundation.HolodexService,
		ytStack.GetService(),
		nil,
		ytStack.GetStatsRepository(),
		communityShortsOpsRepository,
		activity.NewActivityLogger("", logger),
		sharedmodules.BuildSettingsService(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger),
		settingsApplier,
		aclService,
		systemCollector,
		templateAdmin,
		majorEventTriggerClient,
		majorEventTriggerClient,
		logger,
	)
}

func buildAdminAPISystemCollector(appConfig *config.Config) *system.Collector {
	return system.NewCollector([]system.ServiceEndpoint{
		{Name: "llm-scheduler", URL: appConfig.Services.LLMSchedulerHealthURL},
		{Name: "twentyq", URL: appConfig.Services.GameBotTwentyQHealthURL},
		{Name: "turtlesoup", URL: appConfig.Services.GameBotTurtleHealthURL},
	}, system.WithServiceName("hololive-admin-api"))
}

func buildAdminAPITemplateAdmin(infra *sharedmodules.InfraModule, logger *slog.Logger) *template.AdminService {
	templateRenderer := template.NewRenderer(infra.Postgres.GetGormDB(), logger)
	return template.NewAdminService(
		repository.NewTemplateRepository(infra.Postgres.GetGormDB(), logger),
		templateRenderer,
		logger,
	)
}

func buildAdminAPICommunityShortsOpsRepository(infra *sharedmodules.InfraModule) server.YouTubeCommunityShortsOpsRepository {
	if infra.Postgres == nil || infra.Postgres.GetGormDB() == nil {
		return nil
	}
	return outbox.NewDeliveryTelemetryRepository(infra.Postgres.GetGormDB())
}

func buildAdminAPIRouter(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	authService *authsvc.Service,
	handler *server.Handler,
	logger *slog.Logger,
) (*gin.Engine, error) {
	return apphttp.ProvideAPIRouter(
		ctx,
		appConfig,
		logger,
		handler.DomainHandlers(),
		server.NewAuthHandler(authService, logger),
		nil,
		nil,
		infra.Cache,
	)
}

func registerAdminAPIInternalAlarmRoutes(
	router *gin.Engine,
	appConfig *config.Config,
	alarmMode *alarmModeComponents,
	logger *slog.Logger,
) {
	if alarmMode.AlarmCRUD == nil {
		return
	}

	alarmAPI := sharedalarm.NewHandler(alarmMode.AlarmCRUD, logger)
	internalAlarm := router.Group("")
	internalAlarm.Use(middleware.APIKeyAuthMiddleware(appConfig.Server.APIKey))
	alarmAPI.RegisterInternalRoutes(internalAlarm)
}

func buildScraperHolodexProfileFoundation(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*scraperHolodexProfileFoundation, error) {
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
	sharedRL, err := providers.ProvideYouTubeProducerRateLimiter(infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube producer rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperService(
		infra.Cache,
		memberServiceAdapter,
		scraper.ProxyConfig{Enabled: appConfig.Scraper.ProxyEnabled, URL: appConfig.Scraper.ProxyURL},
		sharedRL,
		logger,
	)

	holodexService, err := providers.ProvideHolodexService(
		appConfig.Holodex.BaseURL,
		appConfig.Holodex.APIKey,
		infra.Cache,
		scraperService,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	profileService, err := providers.ProvideProfileService(ctx, infra.Cache, memberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("provide profile service: %w", err)
	}

	return &scraperHolodexProfileFoundation{
		HolodexService:       holodexService,
		MemberServiceAdapter: memberServiceAdapter,
		ProfileService:       profileService,
		SharedRL:             sharedRL,
	}, nil
}

func buildAlarmModeComponents(
	ctx context.Context,
	appConfig *config.Config,
	cacheClient cache.Client,
	holodexService *holodex.Service,
	memberData member.DataProvider,
	alarmRepository *sharedalarm.Repository,
	logger *slog.Logger,
) (*alarmModeComponents, error) {
	chzzkClient := chzzk.NewClient(nil, "", logger)
	if strings.TrimSpace(appConfig.Chzzk.ClientID) != "" || strings.TrimSpace(appConfig.Chzzk.ClientSecret) != "" {
		chzzkClient = chzzk.NewClientWithConfig(chzzk.ClientConfig{
			HTTPClient:   nil,
			ClientID:     appConfig.Chzzk.ClientID,
			ClientSecret: appConfig.Chzzk.ClientSecret,
			Logger:       logger,
		})
	}
	twitchClient := twitch.NewClient(twitch.ClientConfig{
		HTTPClient:   nil,
		ClientID:     appConfig.Twitch.ClientID,
		ClientSecret: appConfig.Twitch.ClientSecret,
	}, logger)
	resolved := sharedmodules.ResolvePersistedTargetMinutes(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger)
	alarmService, err := notification.NewAlarmService(cacheClient, holodexService, chzzkClient, twitchClient, memberData, alarmRepository, logger, resolved)
	if err != nil {
		return nil, fmt.Errorf("create alarm service: %w", err)
	}
	if err := alarmService.WarmCacheFromDB(ctx); err != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", err))
	}

	return &alarmModeComponents{
		AlarmCRUD:        alarmService,
		AlarmService:     alarmService,
		ChzzkClient:      chzzkClient,
		TwitchClient:     twitchClient,
		MemberDataSource: memberData,
	}, nil
}
