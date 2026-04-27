package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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

func BuildAdminAPIRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*AdminAPIRuntime, error) {
	ctx, err := normalizeRuntimeBuildInputs(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: build infra module: %w", err)
	}

	foundation, err := buildScraperHolodexProfileFoundation(ctx, cfg, infra, logger)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build admin api runtime: foundation: %w", err)
	}

	alarmRepo := sharedalarm.NewRepository(infra.Postgres, logger)
	alarmMode, err := buildAlarmModeComponents(ctx, cfg, infra.Cache, foundation.HolodexService, foundation.MemberServiceAdapter, alarmRepo, logger)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build admin api runtime: alarm mode: %w", err)
	}

	aclService, err := acl.NewACLService(
		ctx,
		infra.Postgres,
		infra.Cache,
		logger,
		cfg.Kakao.ACLEnabled,
		acl.ParseACLMode(cfg.Kakao.ACLMode),
		cfg.Kakao.Rooms,
	)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build admin api runtime: acl service: %w", err)
	}

	statsRepo := ytstats.NewYouTubeStatsRepository(infra.Postgres, logger)
	ytStack := sharedmodules.BuildYouTubeAPIStack(ctx, sharedmodules.YouTubeAPIStackParams{
		YouTubeConfig:   cfg.YouTube,
		ScraperConfig:   cfg.Scraper,
		CacheService:    infra.Cache,
		StatsRepo:       statsRepo,
		SharedRateLimit: foundation.SharedRL,
		Logger:          logger,
	})
	templateRenderer := template.NewRenderer(infra.Postgres.GetGormDB(), logger)
	templateAdmin := template.NewAdminService(
		repository.NewTemplateRepository(infra.Postgres.GetGormDB(), logger),
		templateRenderer,
		logger,
	)

	authCfg := authsvc.DefaultConfig()
	authCfg.AutoPrepareSchema = cfg.Postgres.AutoPrepareSchema
	authService, err := authsvc.NewService(ctx, infra.Postgres.GetGormDB(), infra.Cache, logger, authCfg)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build admin api runtime: auth service: %w", err)
	}

	localSettingsApplier := sharedsettings.NewLocalSettingsApplier(
		ytStack.GetService(),
		foundation.HolodexService,
		nil,
		alarmMode.AlarmCRUD,
	)
	settingsApplier := newBotSettingsApplier(localSettingsApplier, nil, logger)

	var majorEventTriggerClient *triggerclient.Client
	if strings.TrimSpace(cfg.LLMSchedulerURL) != "" {
		majorEventTriggerClient = triggerclient.NewClient(cfg.LLMSchedulerURL, cfg.Server.APIKey, logger)
		settingsApplier = newBotSettingsApplier(localSettingsApplier, majorEventTriggerClient, logger)
	} else {
		logger.Warn("LLM scheduler URL not configured; trigger routes and membernews run-now are disabled", slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"))
	}

	systemCollector := system.NewCollector([]system.ServiceEndpoint{
		{Name: "llm-scheduler", URL: cfg.Services.LLMSchedulerHealthURL},
		{Name: "twentyq", URL: cfg.Services.GameBotTwentyQHealthURL},
		{Name: "turtlesoup", URL: cfg.Services.GameBotTurtleHealthURL},
	}, system.WithServiceName("hololive-admin-api"))

	var communityShortsOpsRepo server.YouTubeCommunityShortsOpsRepository
	if infra.Postgres != nil && infra.Postgres.GetGormDB() != nil {
		communityShortsOpsRepo = outbox.NewDeliveryTelemetryRepository(infra.Postgres.GetGormDB())
	}

	handler := server.NewAPIHandler(
		infra.MemberRepo,
		infra.MemberCache,
		infra.Cache,
		foundation.ProfileService,
		alarmMode.AlarmCRUD,
		foundation.HolodexService,
		ytStack.GetService(),
		nil,
		ytStack.GetStatsRepo(),
		communityShortsOpsRepo,
		activity.NewActivityLogger("", logger),
		sharedmodules.BuildSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger),
		settingsApplier,
		aclService,
		systemCollector,
		templateAdmin,
		majorEventTriggerClient,
		majorEventTriggerClient,
		logger,
	)

	router, err := apphttp.ProvideAPIRouter(
		ctx,
		cfg,
		logger,
		handler.DomainHandlers(),
		server.NewAuthHandler(authService, logger),
		nil,
		nil,
		infra.Cache,
	)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build admin api runtime: provide api router: %w", err)
	}

	if alarmMode.AlarmCRUD != nil {
		alarmAPI := sharedalarm.NewAPIHandler(alarmMode.AlarmCRUD, logger)
		internalAlarm := router.Group("")
		internalAlarm.Use(middleware.APIKeyAuthMiddleware(cfg.Server.APIKey))
		alarmAPI.RegisterInternalRoutes(internalAlarm)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	return &AdminAPIRuntime{
		Config:     cfg,
		Logger:     logger,
		ServerAddr: addr,
		HttpServer: sharedserver.NewH2CServer(addr, router, "hololive-admin-api.http"),
		Managed:    lifecycle.NewManaged(infra.Cleanup),
	}, nil
}

func buildScraperHolodexProfileFoundation(
	ctx context.Context,
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*scraperHolodexProfileFoundation, error) {
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, infra.MemberCache, logger)
	sharedRL, err := providers.ProvideYouTubeScraperRateLimiter(infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube scraper rate limiter: %w", err)
	}

	scraperService := providers.ProvideScraperService(
		infra.Cache,
		memberServiceAdapter,
		scraper.ProxyConfig{Enabled: cfg.Scraper.ProxyEnabled, URL: cfg.Scraper.ProxyURL},
		sharedRL,
		logger,
	)

	holodexService, err := providers.ProvideHolodexService(
		cfg.Holodex.BaseURL,
		cfg.Holodex.APIKey,
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
	cfg *config.Config,
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	memberData member.DataProvider,
	alarmRepo *sharedalarm.Repository,
	logger *slog.Logger,
) (*alarmModeComponents, error) {
	chzzkClient := chzzk.NewClient(nil, "", logger)
	if strings.TrimSpace(cfg.Chzzk.ClientID) != "" || strings.TrimSpace(cfg.Chzzk.ClientSecret) != "" {
		chzzkClient = chzzk.NewClientWithConfig(chzzk.ClientConfig{
			HTTPClient:   nil,
			ClientID:     cfg.Chzzk.ClientID,
			ClientSecret: cfg.Chzzk.ClientSecret,
			Logger:       logger,
		})
	}
	twitchClient := twitch.NewClient(twitch.ClientConfig{
		HTTPClient:   nil,
		ClientID:     cfg.Twitch.ClientID,
		ClientSecret: cfg.Twitch.ClientSecret,
	}, logger)
	resolved := sharedmodules.ResolvePersistedTargetMinutes(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)
	alarmService, err := notification.NewAlarmService(cacheSvc, holodexSvc, chzzkClient, twitchClient, memberData, alarmRepo, logger, resolved)
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
