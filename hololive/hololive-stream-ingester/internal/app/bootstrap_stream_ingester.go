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

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

// streamIngesterInfrastructure: stream-ingester 전용 인프라 (alarm/ACL/activity 미포함).
type streamIngesterInfrastructure struct {
	cacheService     *cache.Service
	postgresService  *database.PostgresService
	membersData      domain.MemberDataProvider
	irisClient       iris.Client
	settingsService  *settings.Service
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
	messageStack := providers.ProvideMessageStack(cfg.Bot.Prefix, templateRenderer)

	holodexAPIKeys := providers.ProvideHolodexAPIKeys(cfg.Holodex)
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(infra.memberCache, logger)
	membersData := providers.ProvideMembersData(memberServiceAdapter)
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

	youTubeStatsRepository := providers.ProvideYouTubeStatsRepository(infra.postgresService, logger)
	// stream-ingester는 alarm dispatch가 없으므로 alarmSvc=nil로 전달
	youTubeStack := providers.ProvideYouTubeStack(ctx, cfg.YouTube, cfg.Scraper, infra.cacheService, holodexService, memberServiceAdapter, youTubeStatsRepository, nil, irisClient, messageStack.Formatter, sharedRL, logger)

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

// buildStreamIngesterYouTubeComponents: stream-ingester 전용 YouTube 컴포넌트를 구성한다.
func buildStreamIngesterYouTubeComponents(
	scraperCfg config.ScraperConfig,
	postgresService *database.PostgresService,
	membersData domain.MemberDataProvider,
	cacheService *cache.Service,
	irisClient iris.Client,
	templateRenderer *template.Renderer,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) (*poller.Scheduler, *outbox.Dispatcher) {
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: scraperCfg.ProxyEnabled,
		URL:     scraperCfg.ProxyURL,
	}

	scraperScheduler := providers.ProvideScraperScheduler(
		postgresService,
		membersData,
		providers.DefaultPollerIntervals(),
		[]string{},
		scraperProxyConfig,
		sharedRL,
		cacheService,
		logger,
	)

	outboxDispatcher := outbox.NewDispatcher(
		postgresService.GetGormDB(),
		cacheService,
		irisClient,
		templateRenderer,
		logger,
		outbox.DefaultConfig(),
	)

	return scraperScheduler, outboxDispatcher
}

// buildStreamIngesterConfigSubscriber: stream-ingester 전용 ConfigSubscriber를 구성한다.
// alarm_advance_minutes 설정은 stream-ingester에서 불필요하므로 scraper_proxy만 처리한다.
func buildStreamIngesterConfigSubscriber(
	cacheService *cache.Service,
	settingsService *settings.Service,
	holodexService *holodex.Service,
	ytStack *providers.YouTubeStack,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := func(update configsub.ConfigUpdate) {
		switch update.Type {
		case "scraper_proxy":
			var payload struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.Unmarshal(update.Payload, &payload); err != nil {
				logger.Warn("Failed to unmarshal scraper_proxy payload", slog.Any("error", err))
				return
			}
			applyScraperProxyToggle(payload.Enabled, ProvideYouTubeService(ytStack), holodexService, scraperScheduler, logger)
			current := settingsService.Get()
			current.ScraperProxyEnabled = payload.Enabled
			if err := settingsService.Update(current); err != nil {
				logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
			}

		case "alarm_advance_minutes":
			// stream-ingester는 alarm dispatch를 담당하지 않으므로 무시
			logger.Debug("Ignoring alarm_advance_minutes config update (stream-ingester)")

		default:
			logger.Warn("Unknown config update type", slog.String("type", update.Type))
		}
	}

	return configsub.New(cacheService.GetClient(), applyFn, logger)
}

// StreamIngesterRuntime: stream-ingester 전용 런타임 (YouTube/스크래퍼/PhotoSync/Outbox).
type StreamIngesterRuntime struct {
	Config *config.Config
	Logger *slog.Logger

	Scheduler        *youtube.Scheduler
	ScraperScheduler *poller.Scheduler
	PhotoSync        *holodex.PhotoSyncService
	OutboxDispatcher *outbox.Dispatcher
	ConfigSubscriber *configsub.Subscriber

	ServerAddr string
	HttpServer *http.Server

	ingestionLease *providers.IngestionLease
	cleanup        func()
}

// Close: 리소스를 정리합니다.
func (r *StreamIngesterRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// BuildStreamIngesterRuntime: stream-ingester 런타임을 구성합니다.
func BuildStreamIngesterRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}

	if !cfg.Bot.IngestionEnabled {
		return nil, fmt.Errorf("stream ingester requires BOT_INGESTION_ENABLED=true")
	}
	logger.Info("Stream-ingester ingestion runtime enabled",
		slog.String("event", "stream_ingestion_enabled"),
		slog.String("env", "BOT_INGESTION_ENABLED=true"),
		slog.String("lock_key", providers.IngestionLeaseKey),
	)

	infra, err := initStreamIngesterInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	ingestionLeaseRef, err := providers.AcquireIngestionLease(ctx, infra.cacheService, "stream-ingester", logger)
	if err != nil {
		infra.cleanupDB()
		infra.cleanupCache()
		return nil, fmt.Errorf("acquire ingestion lease: %w", err)
	}

	scraperScheduler, outboxDispatcher := buildStreamIngesterYouTubeComponents(
		cfg.Scraper,
		infra.postgresService,
		infra.membersData,
		infra.cacheService,
		infra.irisClient,
		infra.templateRenderer,
		infra.sharedRL,
		logger,
	)
	youtubeScheduler := infra.ytStack.Scheduler
	configSubscriber := buildStreamIngesterConfigSubscriber(
		infra.cacheService,
		infra.settingsService,
		infra.holodexService,
		infra.ytStack,
		scraperScheduler,
		logger,
	)

	desiredProxyState := infra.settingsService.Get().ScraperProxyEnabled
	applyScraperProxyToggle(
		desiredProxyState,
		ProvideYouTubeService(infra.ytStack),
		infra.holodexService,
		scraperScheduler,
		logger,
	)

	httpServer, err := buildStreamIngesterHTTPServer(ctx, cfg, logger)
	if err != nil {
		infra.cleanupDB()
		infra.cleanupCache()
		return nil, err
	}

	cleanup := func() {
		infra.cleanupDB()
		infra.cleanupCache()
	}

	return &StreamIngesterRuntime{
		Config:           cfg,
		Logger:           logger,
		Scheduler:        youtubeScheduler,
		ScraperScheduler: scraperScheduler,
		PhotoSync:        infra.photoSync,
		OutboxDispatcher: outboxDispatcher,
		ConfigSubscriber: configSubscriber,
		ServerAddr:       ProvideAPIAddr(cfg),
		HttpServer:       httpServer,
		ingestionLease:   ingestionLeaseRef,
		cleanup:          cleanup,
	}, nil
}

func buildStreamIngesterHTTPServer(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*http.Server, error) {
	router, err := ProvideHealthOnlyRouter(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("build stream ingester router: %w", err)
	}
	return ProvideAPIServer(ProvideAPIAddr(cfg), router), nil
}

// Run: SIGINT/SIGTERM 신호를 대기하며 graceful shutdown을 수행합니다. (블로킹)
func (r *StreamIngesterRuntime) Run() {
	if r == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)

	if r.ConfigSubscriber != nil {
		go r.ConfigSubscriber.Run(ctx)
		r.Logger.Info("Config subscriber started")
	}
	if r.ingestionLease != nil {
		go r.ingestionLease.StartRenewLoop(ctx, errCh)
	}

	if r.Scheduler != nil {
		r.Scheduler.Start(ctx)
		r.Logger.Info("YouTube ingestion scheduler started")
	}
	if r.ScraperScheduler != nil {
		r.ScraperScheduler.Start(ctx)
		r.Logger.Info("Scraper scheduler started")
	}
	if r.OutboxDispatcher != nil {
		r.OutboxDispatcher.Start(ctx)
		r.Logger.Info("YouTube outbox dispatcher started")
	}
	if r.PhotoSync != nil {
		go r.PhotoSync.Start(ctx)
		r.Logger.Info("Photo sync service started")
	}

	go func() {
		if err := r.HttpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()
	r.Logger.Info("Stream Ingester HTTP server started",
		slog.String("addr", r.ServerAddr),
	)

	select {
	case sig := <-sigCh:
		r.Logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
	case err := <-errCh:
		r.Logger.Error("Server error", slog.Any("error", err))
	}

	cancel()
	r.shutdown()
}

func (r *StreamIngesterRuntime) shutdown() {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)
	defer shutdownCancel()

	if r.Scheduler != nil {
		r.Scheduler.Stop()
	}
	if r.ScraperScheduler != nil {
		r.ScraperScheduler.Stop()
	}
	if r.HttpServer != nil {
		if err := r.HttpServer.Shutdown(shutdownCtx); err != nil {
			r.Logger.Error("Stream ingester HTTP shutdown failed", slog.Any("error", err))
		}
	}
	if r.ingestionLease != nil {
		if err := r.ingestionLease.Release(shutdownCtx); err != nil {
			r.Logger.Error("Stream ingester lease release failed", slog.Any("error", err))
		}
	}
}
