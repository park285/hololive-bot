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

	"github.com/kapu/hololive-llm-sched/internal/service/majorevent"
	mescheduler "github.com/kapu/hololive-llm-sched/internal/service/majorevent/scheduler"
	mescraper "github.com/kapu/hololive-llm-sched/internal/service/majorevent/scraper"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

const (
	configUpdateMemberNewsWeeklyRunNow = "membernews_weekly_run_now"
)

// LLMSchedulerRuntime: llm-scheduler 전용 런타임
type LLMSchedulerRuntime struct {
	Config *config.LLMSchedulerConfig
	Logger *slog.Logger

	DeliveryDispatcher         *delivery.Dispatcher
	MajorEventScheduler        *mescheduler.Scheduler
	MajorEventMonthlyScheduler *mescheduler.MonthlyScheduler
	MajorEventScraperScheduler *mescraper.RuntimeScheduler
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
		r.MajorEventScraperScheduler.Start(ctx)
		r.Logger.Info("Major event scraper runtime scheduler started",
			slog.String("feed_schedule", "daily 04:00 KST"),
			slog.String("maintenance_expire_schedule", "daily 05:00 KST"),
			slog.String("maintenance_link_check_interval", "12h"))
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
		r.Logger.Info("Major event scraper runtime scheduler stopped")
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
	cacheService cache.Client,
	postgresService database.Client,
) (*LLMSchedulerRuntime, error) {
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("init member cache: %w", err)
	}
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(memberCache, logger)
	memberDataProvider := providers.ProvideMembersData(memberServiceAdapter)

	templateRenderer := template.NewRenderer(postgresService.GetGormDB(), logger)
	formatter := newLLMSchedulerFormatter(cfg.Bot.Prefix, templateRenderer, logger)
	majorEventRepo := majorevent.NewRepository(postgresService, logger)
	if cfg.Postgres.AutoPrepareSchema {
		if createErr := majorEventRepo.CreateTable(ctx); createErr != nil {
			logger.Error("Failed to create major_event_subscriptions table", slog.String("error", createErr.Error()))
		}
	}
	memberNewsService := initMemberNewsService(ctx, cfg.Cliproxy, cfg.LLM, cfg.Exa, postgresService, cacheService, memberDataProvider, logger)

	deliveryLocker := ProvideDeliveryLocker(cacheService, logger)
	outboxRepo := ProvideOutboxRepository(postgresService, logger)
	irisClient := providers.ProvideIrisClient(cfg.Iris, logger)
	deliverySender := providers.NewIrisMessageSender(irisClient)
	deliveryDispatcher := ProvideDeliveryDispatcher(outboxRepo, deliverySender, logger)

	majorEventLLMClient := ProvideMajorEventLLMClient(cfg.Cliproxy, logger)
	majorEventReviewer := ProvideMajorEventReviewerClient(cfg.Cliproxy, cfg.LLM, logger)
	majorEventAdjudicator := ProvideMajorEventAdjudicatorClient(cfg.Cliproxy, cfg.LLM, logger)
	exaSearcher := provideExaSearcher(cfg.Exa, logger)
	summarizer := provideEventSummarizer(cfg.LLM.MajorEvent, majorEventLLMClient, majorEventReviewer, majorEventAdjudicator, cacheService, exaSearcher, logger)

	majorEventScheduler, majorEventMonthlyScheduler, majorEventScraperScheduler := buildMajorEventComponents(
		ctx,
		majorEventRepo,
		formatter,
		summarizer,
		deliveryLocker,
		outboxRepo,
		logger,
		cfg.Postgres.AutoPrepareSchema,
	)
	memberNewsScheduler, memberNewsMonthlyScheduler := buildMemberNewsComponents(memberNewsService, formatter, deliveryLocker, outboxRepo, logger)

	triggerHandler := ProvideTriggerHandler(majorEventScheduler, majorEventMonthlyScheduler, memberNewsScheduler, logger)
	httpServer, err := buildLLMSchedulerHTTPServer(ctx, cfg.Server.Port, logger, triggerHandler, cfg.Server.APIKey, majorEventRepo, memberNewsService)
	if err != nil {
		return nil, err
	}
	configSubscriber := buildLLMSchedulerConfigSubscriber(ctx, cacheService, memberNewsScheduler, logger)

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
	apiKey string,
	majorEventRepo *majorevent.Repository,
	memberNewsService *membernews.Service,
) (*http.Server, error) {
	router, err := ProvideTriggerRouter(ctx, logger, triggerHandler, apiKey)
	if err != nil {
		return nil, fmt.Errorf("build llm scheduler router: %w", err)
	}

	//nolint:contextcheck // gin handlers use per-request context via c.Request.Context()
	registerMajorEventInternalRoutes(router, apiKey, majorEventRepo)
	//nolint:contextcheck // gin handlers use per-request context via c.Request.Context()
	registerMemberNewsInternalRoutes(router, apiKey, memberNewsService)

	addr := fmt.Sprintf(":%d", port)
	return ProvideAPIServer(addr, router), nil
}

func buildLLMSchedulerConfigSubscriber(
	ctx context.Context,
	cacheService cache.Client,
	memberNewsScheduler *membernews.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	applyFn := func(update configsub.ConfigUpdate) {
		switch update.Type {
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
