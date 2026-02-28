package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kapu/hololive-admin/internal/server"
	"github.com/kapu/hololive-admin/internal/service/configpub"
	"github.com/kapu/hololive-admin/internal/service/trigger"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/repository"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// AdminAPIRuntime: admin-api 전용 런타임
type AdminAPIRuntime struct {
	Config     *config.AdminAPIConfig
	Logger     *slog.Logger
	cleanup    func()
	httpServer *http.Server
}

// Close: 리소스를 정리합니다.
func (r *AdminAPIRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// Run: SIGINT/SIGTERM 신호를 대기하며 graceful shutdown을 수행합니다. (블로킹)
func (r *AdminAPIRuntime) Run() {
	if r == nil {
		return
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)

	// HTTP 서버 시작
	go func() {
		if err := r.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()
	r.Logger.Info("Admin API HTTP server started",
		slog.String("addr", r.httpServer.Addr))

	select {
	case sig := <-sigCh:
		r.Logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
	case err := <-errCh:
		r.Logger.Error("Server error", slog.Any("error", err))
	}

	r.Logger.Info("Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)
	defer shutdownCancel()

	if err := r.httpServer.Shutdown(shutdownCtx); err != nil {
		r.Logger.Error("HTTP server shutdown error", slog.Any("error", err))
	} else {
		r.Logger.Info("HTTP server stopped")
	}

	r.Logger.Info("Shutdown complete")
}

// BuildAdminAPIRuntime: admin-api 런타임을 구성합니다.
func BuildAdminAPIRuntime(ctx context.Context, cfg *config.AdminAPIConfig, logger *slog.Logger) (*AdminAPIRuntime, error) {
	// 1. Valkey 캐시 초기화
	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, cfg.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("init cache: %w", err)
	}
	cacheService := providers.ProvideCacheService(cacheResources)

	// 2. PostgreSQL 초기화
	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		cleanupCache()
		return nil, fmt.Errorf("init database: %w", err)
	}
	postgresService := providers.ProvidePostgresService(databaseResources)

	cleanup := func() {
		cleanupDB()
		cleanupCache()
	}

	runtime, err := buildAdminComponents(ctx, cfg, logger, cacheService, postgresService, cleanup)
	if err != nil {
		cleanup()
		return nil, err
	}

	return runtime, nil
}

// buildAdminComponents: admin-api 컴포넌트를 조립합니다.
func buildAdminComponents(
	ctx context.Context,
	cfg *config.AdminAPIConfig,
	logger *slog.Logger,
	cacheService *cache.Service,
	postgresService *database.PostgresService,
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
	alarmCRUD := alarm.NewClient(cfg.AlarmDispatcherURL, logger)

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
	triggerClient := trigger.NewClient(cfg.LLMSchedulerURL, logger)

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
	cacheService *cache.Service,
	memberServiceAdapter *member.ServiceAdapter,
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

func buildAdminScraperRateLimiter(cacheService *cache.Service, logger *slog.Logger) (*scraper.RateLimiter, error) {
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

// buildAdminHTTPServer: admin-api 전용 HTTP 서버를 구성합니다.
func buildAdminHTTPServer(
	ctx context.Context,
	cfg *config.AdminAPIConfig,
	logger *slog.Logger,
	domainHandlers *server.DomainAPIHandlers,
	authHandler *server.AuthHandler,
) (*http.Server, error) {
	// admin-api에서도 ProvideAPIRouter를 재사용하기 위해 config.Config로 변환
	fullCfg := &config.Config{
		Server:    cfg.Server,
		CORS:      cfg.CORS,
		Telemetry: cfg.Telemetry,
	}

	adminRouter, err := ProvideAPIRouter(ctx, fullCfg, logger, domainHandlers, authHandler, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create api router: %w", err)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           sharedserver.WrapH2C(adminRouter),
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}, nil
}
