package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

// AlarmDispatcherRuntime: alarm-dispatcher 전용 런타임
type AlarmDispatcherRuntime struct {
	Config  *config.AlarmDispatcherConfig
	Logger  *slog.Logger
	cleanup func()

	alarmService     *notification.AlarmService
	queueDispatcher  *notification.AlarmQueueDispatcher
	configSubscriber *configsub.Subscriber
	apiHandler       *alarm.APIHandler
	httpServer       *http.Server
}

// Close: 리소스를 정리합니다.
func (r *AlarmDispatcherRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// Run: SIGINT/SIGTERM 신호를 대기하며 graceful shutdown을 수행합니다. (블로킹)
func (r *AlarmDispatcherRuntime) Run() {
	if r == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)

	// ConfigSubscriber 시작 (설정 변경 Pub/Sub 수신)
	if r.configSubscriber != nil {
		go r.configSubscriber.Run(ctx)
		r.Logger.Info("Config subscriber started")
	}

	// AlarmQueueDispatcher 시작
	if r.queueDispatcher != nil {
		go r.queueDispatcher.Run(ctx)
		r.Logger.Info("Alarm queue dispatcher started")
	}

	// HTTP 서버 시작
	go func() {
		if err := r.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()
	r.Logger.Info("Alarm dispatcher HTTP server started",
		slog.String("addr", r.httpServer.Addr))

	select {
	case sig := <-sigCh:
		r.Logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
	case err := <-errCh:
		r.Logger.Error("Server error", slog.Any("error", err))
	}

	r.Logger.Info("Shutting down gracefully...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)
	defer shutdownCancel()

	// HTTP 서버 종료 (15초 대기)
	if err := r.httpServer.Shutdown(shutdownCtx); err != nil {
		r.Logger.Error("HTTP server shutdown error", slog.Any("error", err))
	} else {
		r.Logger.Info("HTTP server stopped")
	}

	// AlarmService 전역 레지스트리 종료
	if err := notification.CloseAllAlarmServices(shutdownCtx); err != nil {
		r.Logger.Error("Alarm service shutdown error", slog.Any("error", err))
	} else {
		r.Logger.Info("Alarm services stopped")
	}

	r.Logger.Info("Shutdown complete")
}

// BuildAlarmDispatcherRuntime: alarm-dispatcher 런타임을 구성합니다.
func BuildAlarmDispatcherRuntime(ctx context.Context, cfg *config.AlarmDispatcherConfig, logger *slog.Logger) (*AlarmDispatcherRuntime, error) {
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

	runtime, err := buildDispatcherComponents(ctx, cfg, logger, cacheService, postgresService)
	if err != nil {
		cleanup()
		return nil, err
	}

	runtime.cleanup = cleanup
	return runtime, nil
}

// buildDispatcherComponents: 인프라 리소스를 기반으로 dispatcher 컴포넌트를 조립합니다.
func buildDispatcherComponents(
	ctx context.Context,
	cfg *config.AlarmDispatcherConfig,
	logger *slog.Logger,
	cacheService *cache.Service,
	postgresService *database.PostgresService,
) (*AlarmDispatcherRuntime, error) {
	// 멤버 캐시 초기화 (알람 이름 조회 및 member DB 초기화 포함)
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("init member cache: %w", err)
	}
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(memberCache, logger)

	// Iris 클라이언트 (알림 발송용)
	irisClient := providers.ProvideIrisClient(cfg.Iris, logger)

	// Holodex 서비스 (GetNextStreamInfo용)
	sharedRL := scraper.NewRateLimiter(3 * time.Second)
	if constants.YouTubeScraperDistributedRateLimitConfig.Enabled {
		distributedLimiter, distErr := ratelimit.NewSlidingWindowLimiter(
			cacheService,
			constants.YouTubeScraperDistributedRateLimitConfig.KeyPrefix,
			logger,
		)
		if distErr != nil {
			return nil, fmt.Errorf("init scraper distributed rate limiter: %w", distErr)
		}
		if distErr := sharedRL.ConfigureDistributed(
			distributedLimiter,
			constants.YouTubeScraperDistributedRateLimitConfig.Limit,
			constants.YouTubeScraperDistributedRateLimitConfig.Window,
		); distErr != nil {
			return nil, fmt.Errorf("configure scraper distributed rate limiter: %w", distErr)
		}
	}
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: cfg.Scraper.ProxyEnabled,
		URL:     cfg.Scraper.ProxyURL,
	}
	holodexAPIKeys := providers.ProvideHolodexAPIKeys(cfg.Holodex)
	scraperService := providers.ProvideScraperService(cacheService, memberServiceAdapter, scraperProxyConfig, sharedRL, logger)
	holodexService, err := providers.ProvideHolodexService(cfg.Holodex.BaseURL, holodexAPIKeys, cacheService, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("init holodex service: %w", err)
	}

	// 알람 서비스 조립 (Chzzk/Twitch 클라이언트 포함)
	alarmRepository := providers.ProvideAlarmRepository(postgresService, logger)
	alarmSvc, err := buildDispatcherAlarmService(cfg, cacheService, holodexService, memberServiceAdapter, alarmRepository, logger)
	if err != nil {
		return nil, err
	}

	// 알람 캐시 워밍업
	if warmErr := alarmSvc.WarmCacheFromDB(ctx); warmErr != nil {
		logger.Warn("Failed to warm alarm cache from DB", slog.Any("error", warmErr))
	}

	// Chzzk/Twitch 플랫폼 매핑 동기화
	if syncErr := alarmSvc.SyncPlatformMappings(ctx); syncErr != nil {
		logger.Warn("Failed to sync platform mappings", slog.Any("error", syncErr))
	}

	// ResponseFormatter 생성 (AlarmQueueDispatcher 의존)
	templateRenderer := template.NewRenderer(postgresService.GetGormDB(), logger)
	msgStack := providers.ProvideMessageStack("", templateRenderer)

	// AlarmQueueDispatcher 생성
	queueDispatcher := providers.ProvideAlarmQueueDispatcher(
		cfg.Notification.AlarmQueueConsumerEnabled,
		cacheService,
		alarmSvc,
		irisClient,
		msgStack.Formatter,
		logger,
	)

	// ConfigSubscriber: alarm_advance_minutes 설정 변경 수신
	dispatcherConfigSub := buildDispatcherConfigSubscriber(cacheService, alarmSvc, logger)

	// alarm internal API 핸들러 및 HTTP 서버 구성
	apiHandler := alarm.NewAPIHandler(alarmSvc, logger)
	httpServer := buildDispatcherHTTPServer(cfg.Port, apiHandler)

	return &AlarmDispatcherRuntime{
		Config:           cfg,
		Logger:           logger,
		alarmService:     alarmSvc,
		queueDispatcher:  queueDispatcher,
		configSubscriber: dispatcherConfigSub,
		apiHandler:       apiHandler,
		httpServer:       httpServer,
	}, nil
}

// buildDispatcherAlarmService: Chzzk/Twitch 클라이언트를 포함한 AlarmService를 생성합니다.
func buildDispatcherAlarmService(
	cfg *config.AlarmDispatcherConfig,
	cacheService *cache.Service,
	holodexService *holodex.Service,
	memberServiceAdapter *member.ServiceAdapter,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*notification.AlarmService, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	chzzkClient := providers.ProvideChzzkClient(httpClient, cfg.Chzzk, logger)
	twitchClient := providers.ProvideTwitchClient(cfg.Twitch, logger)
	memberDataProvider := providers.ProvideMembersData(memberServiceAdapter)

	resolved := providers.ResolveAlarmAdvanceMinutes(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)
	alarmSvc, err := providers.ProvideAlarmService(resolved, cacheService, holodexService, chzzkClient, twitchClient, memberDataProvider, alarmRepository, logger)
	if err != nil {
		return nil, fmt.Errorf("build alarm service: %w", err)
	}
	return alarmSvc, nil
}

// buildDispatcherHTTPServer: alarm-dispatcher 전용 HTTP 서버를 구성합니다.
// gin.New() + gin.Recovery() 미들웨어를 사용하며, alarm API 엔드포인트를 등록합니다.
func buildDispatcherHTTPServer(port int, apiHandler *alarm.APIHandler) *http.Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	// /internal/alarm/*, /health, /ready 등록
	rootGroup := engine.Group("")
	apiHandler.RegisterRoutes(rootGroup)

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           engine,
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
	}
}

// buildDispatcherConfigSubscriber: alarm-dispatcher용 ConfigSubscriber를 생성합니다.
// alarm_advance_minutes 타입만 처리합니다.
func buildDispatcherConfigSubscriber(
	cacheService *cache.Service,
	alarmSvc *notification.AlarmService,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := func(update configsub.ConfigUpdate) {
		switch update.Type {
		case "alarm_advance_minutes":
			var payload struct {
				Minutes int `json:"minutes"`
			}
			if err := json.Unmarshal(update.Payload, &payload); err != nil {
				logger.Warn("Failed to unmarshal alarm_advance_minutes payload", slog.Any("error", err))
				return
			}
			targets := alarmSvc.UpdateAlarmAdvanceMinutes(payload.Minutes)
			logger.Info("Applied alarm advance minutes via pub/sub",
				slog.Int("minutes", payload.Minutes),
				slog.Any("targets", targets),
			)

		default:
			logger.Debug("Ignoring config update type for dispatcher", slog.String("type", update.Type))
		}
	}

	return configsub.New(cacheService.GetClient(), applyFn, logger)
}
