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

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
	"github.com/kapu/hololive-shared/pkg/service/template"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

const (
	configUpdateMajorEventScrapeHourKST = "majorevent_scrape_hour_kst"
	configUpdateMajorEventScrapeRunNow  = "majorevent_scrape_run_now"
	configUpdateMemberNewsWeeklyRunNow  = "membernews_weekly_run_now"
)

// LLMSchedulerRuntime: llm-scheduler 전용 런타임
type LLMSchedulerRuntime struct {
	Config *config.LLMSchedulerConfig
	Logger *slog.Logger

	DeliveryDispatcher         *delivery.Dispatcher
	MajorEventScheduler        *majorevent.Scheduler
	MajorEventMonthlyScheduler *majorevent.MonthlyScheduler
	MajorEventScraperScheduler *majorevent.ScraperScheduler
	MemberNewsScheduler        *membernews.Scheduler
	MemberNewsMonthlyScheduler *membernews.MonthlyScheduler

	configSubscriber *configsub.Subscriber
	httpServer       *http.Server
	cleanup          func()
}

// Close: 리소스를 정리합니다.
func (r *LLMSchedulerRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// Run: SIGINT/SIGTERM 신호를 대기하며 graceful shutdown을 수행합니다. (블로킹)
func (r *LLMSchedulerRuntime) Run() {
	if r == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)

	if r.configSubscriber != nil {
		go r.configSubscriber.Run(ctx)
		r.Logger.Info("LLM scheduler config subscriber started")
	}

	r.startSchedulers(ctx)
	r.startHTTPServer(errCh)

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

	r.stopSchedulers()
	if err := r.httpServer.Shutdown(shutdownCtx); err != nil {
		r.Logger.Error("HTTP server shutdown error", slog.Any("error", err))
	} else {
		r.Logger.Info("HTTP server stopped")
	}

	r.Logger.Info("Shutdown complete")
}

func (r *LLMSchedulerRuntime) startHTTPServer(errCh chan<- error) {
	go func() {
		if err := r.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if errCh != nil {
				errCh <- fmt.Errorf("HTTP server error: %w", err)
			}
		}
	}()
	r.Logger.Info("LLM scheduler HTTP server started",
		slog.String("addr", r.httpServer.Addr))
}

func (r *LLMSchedulerRuntime) startSchedulers(ctx context.Context) {
	if r.DeliveryDispatcher != nil {
		r.DeliveryDispatcher.Start(ctx)
		r.Logger.Info("Delivery outbox dispatcher started")
	}

	if r.MajorEventScheduler != nil {
		r.MajorEventScheduler.Start(ctx)
		r.Logger.Info("Major event weekly scheduler started",
			slog.String("schedule", fmt.Sprintf("%s %02d:00 KST",
				constants.MajorEventConfig.ScheduleWeekday, constants.MajorEventConfig.ScheduleHourKST)))
	}

	if r.MajorEventMonthlyScheduler != nil {
		r.MajorEventMonthlyScheduler.Start(ctx)
		r.Logger.Info("Major event monthly scheduler started",
			slog.String("schedule", fmt.Sprintf("%dth %02d:00 KST",
				constants.MajorEventConfig.MonthlyScheduleDay, constants.MajorEventConfig.MonthlyScheduleHourKST)))
	}

	if r.MajorEventScraperScheduler != nil {
		scrapeHourKST := r.MajorEventScraperScheduler.ScrapeHourKST()
		r.MajorEventScraperScheduler.Start(ctx)
		r.Logger.Info("Major event scraper scheduler started",
			slog.String("schedule", fmt.Sprintf("daily %02d:00 KST",
				scrapeHourKST)))

		go func() {
			r.MajorEventScraperScheduler.RunNow(ctx)
			r.Logger.Info("Initial major event scrape completed")
		}()
	}

	if r.MemberNewsScheduler != nil {
		r.MemberNewsScheduler.Start(ctx)
		r.Logger.Info("Member news weekly scheduler started",
			slog.String("schedule", fmt.Sprintf("%s %02d:00 KST",
				membernews.WeeklyScheduleWeekday, membernews.WeeklyScheduleHourKST)))
	}

	if r.MemberNewsMonthlyScheduler != nil {
		r.MemberNewsMonthlyScheduler.Start(ctx)
		r.Logger.Info("Member news monthly scheduler started",
			slog.String("schedule", fmt.Sprintf("%dth %02d:00 KST",
				membernews.MonthlyScheduleDay, membernews.MonthlyScheduleHourKST)))
	}
}

func (r *LLMSchedulerRuntime) stopSchedulers() {
	if r.MajorEventScheduler != nil {
		r.MajorEventScheduler.Stop()
		r.Logger.Info("Major event scheduler stopped")
	}
	if r.MajorEventMonthlyScheduler != nil {
		r.MajorEventMonthlyScheduler.Stop()
		r.Logger.Info("Major event monthly scheduler stopped")
	}
	if r.MajorEventScraperScheduler != nil {
		r.MajorEventScraperScheduler.Stop()
		r.Logger.Info("Major event scraper scheduler stopped")
	}
	if r.MemberNewsScheduler != nil {
		r.MemberNewsScheduler.Stop()
		r.Logger.Info("Member news scheduler stopped")
	}
	if r.MemberNewsMonthlyScheduler != nil {
		r.MemberNewsMonthlyScheduler.Stop()
		r.Logger.Info("Member news monthly scheduler stopped")
	}
}

// BuildLLMSchedulerRuntime: llm-scheduler 런타임을 구성합니다.
func BuildLLMSchedulerRuntime(ctx context.Context, cfg *config.LLMSchedulerConfig, logger *slog.Logger) (*LLMSchedulerRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("llm scheduler config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}

	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, cfg.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("init cache: %w", err)
	}
	cacheService := providers.ProvideCacheService(cacheResources)

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

	runtime, err := buildLLMSchedulerComponents(ctx, cfg, logger, cacheService, postgresService)
	if err != nil {
		cleanup()
		return nil, err
	}
	runtime.cleanup = cleanup
	return runtime, nil
}

func buildLLMSchedulerComponents(
	ctx context.Context,
	cfg *config.LLMSchedulerConfig,
	logger *slog.Logger,
	cacheService *cache.Service,
	postgresService *database.PostgresService,
) (*LLMSchedulerRuntime, error) {
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("init member cache: %w", err)
	}
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(memberCache, logger)
	memberDataProvider := providers.ProvideMembersData(memberServiceAdapter)

	templateRenderer := template.NewRenderer(postgresService.GetGormDB(), logger)
	msgStack := providers.ProvideMessageStack(cfg.Bot.Prefix, templateRenderer)
	majorEventRepo := providers.ProvideMajorEventRepository(ctx, postgresService, logger, cfg.Postgres.AutoPrepareSchema)
	memberNewsService := initMemberNewsService(ctx, cfg.Cliproxy, cfg.LLM, cfg.Exa, postgresService, cacheService, memberDataProvider, logger)

	deliveryLocker := providers.ProvideDeliveryLocker(cacheService, logger)
	outboxRepo := providers.ProvideOutboxRepository(postgresService, logger)
	irisClient := providers.ProvideIrisClient(cfg.Iris, logger)
	deliverySender := providers.NewIrisMessageSender(irisClient)
	deliveryDispatcher := providers.ProvideDeliveryDispatcher(outboxRepo, deliverySender, logger)

	majorEventLLMClient := providers.ProvideMajorEventLLMClient(cfg.Cliproxy, logger)
	majorEventReviewer := providers.ProvideMajorEventReviewerClient(cfg.Cliproxy, cfg.LLM, logger)
	majorEventAdjudicator := providers.ProvideMajorEventAdjudicatorClient(cfg.Cliproxy, cfg.LLM, logger)
	exaSearcher := providers.ProvideExaSearcher(cfg.Exa, logger)
	summarizer := providers.ProvideEventSummarizer(cfg.LLM.MajorEvent, majorEventLLMClient, majorEventReviewer, majorEventAdjudicator, cacheService, exaSearcher, logger)

	majorEventScheduler, majorEventMonthlyScheduler, majorEventScraperScheduler := buildMajorEventComponents(
		ctx,
		cfg.MajorEvent,
		majorEventRepo,
		msgStack.Formatter,
		summarizer,
		deliveryLocker,
		outboxRepo,
		logger,
		cfg.Postgres.AutoPrepareSchema,
	)
	memberNewsScheduler, memberNewsMonthlyScheduler := buildMemberNewsComponents(memberNewsService, msgStack.Formatter, deliveryLocker, outboxRepo, logger)

	triggerHandler := ProvideTriggerHandler(majorEventScheduler, majorEventMonthlyScheduler, memberNewsScheduler, logger)
	httpServer, err := buildLLMSchedulerHTTPServer(ctx, cfg.Server.Port, logger, triggerHandler)
	if err != nil {
		return nil, err
	}
	configSubscriber := buildLLMSchedulerConfigSubscriber(ctx, cacheService, majorEventScraperScheduler, memberNewsScheduler, logger)

	return &LLMSchedulerRuntime{
		Config:                     cfg,
		Logger:                     logger,
		DeliveryDispatcher:         deliveryDispatcher,
		MajorEventScheduler:        majorEventScheduler,
		MajorEventMonthlyScheduler: majorEventMonthlyScheduler,
		MajorEventScraperScheduler: majorEventScraperScheduler,
		MemberNewsScheduler:        memberNewsScheduler,
		MemberNewsMonthlyScheduler: memberNewsMonthlyScheduler,
		configSubscriber:           configSubscriber,
		httpServer:                 httpServer,
	}, nil
}

func buildLLMSchedulerHTTPServer(
	ctx context.Context,
	port int,
	logger *slog.Logger,
	triggerHandler *sharedserver.TriggerHandler,
) (*http.Server, error) {
	router, err := ProvideTriggerRouter(ctx, logger, triggerHandler)
	if err != nil {
		return nil, fmt.Errorf("build llm scheduler router: %w", err)
	}
	addr := fmt.Sprintf(":%d", port)
	return ProvideAPIServer(addr, router), nil
}

func buildLLMSchedulerConfigSubscriber(
	ctx context.Context,
	cacheService *cache.Service,
	majorEventScraperScheduler *majorevent.ScraperScheduler,
	memberNewsScheduler *membernews.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	applyFn := func(update configsub.ConfigUpdate) {
		switch update.Type {
		case configUpdateMajorEventScrapeHourKST:
			if majorEventScraperScheduler == nil {
				logger.Warn("Ignored majorevent scrape hour update: scraper scheduler not initialized")
				return
			}

			var payload struct {
				HourKST int `json:"hour_kst"`
			}
			if err := json.Unmarshal(update.Payload, &payload); err != nil {
				logger.Warn("Failed to unmarshal majorevent_scrape_hour_kst payload", slog.Any("error", err))
				return
			}
			appliedHour := majorEventScraperScheduler.SetScrapeHourKST(payload.HourKST)
			logger.Info("Applied major event scrape hour update",
				slog.Int("requested_hour_kst", payload.HourKST),
				slog.Int("applied_hour_kst", appliedHour),
			)

		case configUpdateMajorEventScrapeRunNow:
			if majorEventScraperScheduler == nil {
				logger.Warn("Ignored majorevent scrape run-now: scraper scheduler not initialized")
				return
			}

			go func() {
				runCtx, cancel := context.WithTimeout(baseCtx, constants.RequestTimeout.BotAlarmCheck)
				defer cancel()
				majorEventScraperScheduler.RunNow(runCtx)
				logger.Info("Major event scrape run-now completed via config update")
			}()

		case configUpdateMemberNewsWeeklyRunNow:
			if memberNewsScheduler == nil {
				logger.Warn("Ignored membernews weekly run-now: scheduler not initialized")
				return
			}

			go func() {
				runCtx, cancel := context.WithTimeout(baseCtx, constants.RequestTimeout.BotAlarmCheck)
				defer cancel()
				if err := memberNewsScheduler.SendWeeklyDigest(runCtx); err != nil {
					logger.Error("Member news weekly run-now failed", slog.Any("error", err))
					return
				}
				logger.Info("Member news weekly run-now completed via config update")
			}()

		default:
			logger.Debug("Ignoring config update type for llm scheduler", slog.String("type", update.Type))
		}
	}

	return configsub.New(cacheService.GetClient(), applyFn, logger)
}
