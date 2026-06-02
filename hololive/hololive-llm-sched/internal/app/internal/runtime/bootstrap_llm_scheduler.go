// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/kapu/hololive-llm-sched/internal/service/majorevent"
	mescheduler "github.com/kapu/hololive-llm-sched/internal/service/majorevent/scheduler"
	mescraper "github.com/kapu/hololive-llm-sched/internal/service/majorevent/scraper"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews"
	mnscheduler "github.com/kapu/hololive-llm-sched/internal/service/membernews/scheduler"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/shared-go/pkg/runtime/httpserver"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

type LLMSchedulerRuntime struct {
	Config *config.LLMSchedulerConfig
	Logger *slog.Logger

	MajorEventScheduler        *mescheduler.Scheduler
	MajorEventMonthlyScheduler *mescheduler.MonthlyScheduler
	MajorEventScraperScheduler *mescraper.RuntimeScheduler
	MemberNewsScheduler        *mnscheduler.Scheduler
	MemberNewsMonthlyScheduler *mnscheduler.MonthlyScheduler

	httpServer *http.Server
	lifecycle.Managed
}

func (r *LLMSchedulerRuntime) Run() {
	if err := lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start: func(ctx context.Context, errCh chan<- error) {
			r.startSchedulers(ctx)
			r.startHTTPServer(errCh)
		},
		OnSignal: func(sig os.Signal) {
			r.Logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
		},
		OnError: func(err error) {
			r.Logger.Error("Server error", slog.Any("error", err))
		},
		BeforeShutdown: func() {
			r.Logger.Info("Shutting down gracefully...")
		},
		Shutdown: r.Shutdown,
	}); err != nil {
		r.Logger.Error("Shutdown completed with errors", slog.Any("error", err))
	}

	r.Logger.Info("Shutdown complete")
}

func (r *LLMSchedulerRuntime) startHTTPServer(errCh chan<- error) {
	httpserver.StartHTTPServer(r.httpServer, r.Logger, errCh)
	r.Logger.Info("LLM scheduler HTTP server started",
		slog.String("addr", r.httpServer.Addr))
}

func (r *LLMSchedulerRuntime) startSchedulers(ctx context.Context) {
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
				mnscheduler.WeeklyScheduleWeekday, mnscheduler.WeeklyScheduleHourKST)))
	}

	if r.MemberNewsMonthlyScheduler != nil {
		r.MemberNewsMonthlyScheduler.Start(ctx)
		r.Logger.Info("Member news monthly scheduler started",
			slog.String("schedule", fmt.Sprintf("%dth %02d:00 KST",
				mnscheduler.MonthlyScheduleDay, mnscheduler.MonthlyScheduleHourKST)))
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

func (r *LLMSchedulerRuntime) Shutdown(ctx context.Context) error {
	var errs []error
	r.stopSchedulers()
	if err := httpserver.ShutdownHTTPServer(ctx, r.httpServer); err != nil {
		r.Logger.Error("HTTP server shutdown error", slog.Any("error", err))
		errs = append(errs, err)
	} else {
		r.Logger.Info("HTTP server stopped")
	}
	return errors.Join(errs...)
}

func BuildLLMSchedulerRuntime(ctx context.Context, schedulerConfig *config.LLMSchedulerConfig, logger *slog.Logger) (*LLMSchedulerRuntime, error) {
	if schedulerConfig == nil {
		return nil, fmt.Errorf("llm scheduler config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}

	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, schedulerConfig.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("init cache: %w", err)
	}
	cacheService := cacheResources.Service

	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, schedulerConfig.Postgres, logger)
	if err != nil {
		cleanupCache()
		return nil, fmt.Errorf("init database: %w", err)
	}
	postgresService := databaseResources.Service

	cleanup := func() {
		cleanupDB()
		cleanupCache()
	}

	runtime, err := buildLLMSchedulerComponents(ctx, schedulerConfig, logger, cacheService, postgresService)
	if err != nil {
		cleanup()
		return nil, err
	}
	runtime.Managed = lifecycle.NewManaged(cleanup)
	return runtime, nil
}

func buildLLMSchedulerComponents(
	ctx context.Context,
	schedulerConfig *config.LLMSchedulerConfig,
	logger *slog.Logger,
	cacheService cache.Client,
	postgresService database.Client,
) (*LLMSchedulerRuntime, error) {
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("init member cache: %w", err)
	}
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, memberCache, logger)
	memberDataProvider := memberServiceAdapter

	templateRenderer := template.NewRenderer(postgresService.GetPool(), logger)
	formatter := newLLMSchedulerFormatter(schedulerConfig.Bot.Prefix, templateRenderer, logger)
	majorEventRepository := buildMajorEventRepository(ctx, postgresService, logger, schedulerConfig.Postgres.AutoPrepareSchema)
	memberNewsService := initMemberNewsService(ctx, schedulerConfig.Cliproxy, schedulerConfig.LLM, schedulerConfig.Exa, postgresService, cacheService, memberDataProvider, logger)

	deliveryModule := buildLLMSchedulerDeliveryModule(cacheService, postgresService, logger)

	summarizer := buildMajorEventSummarizer(schedulerConfig, cacheService, logger)

	majorEventScheduler, majorEventMonthlyScheduler, majorEventScraperScheduler := buildMajorEventComponents(
		ctx,
		majorEventRepository,
		formatter,
		summarizer,
		deliveryModule.Locker,
		deliveryModule.Repository,
		logger,
		schedulerConfig.Postgres.AutoPrepareSchema,
	)
	memberNewsScheduler, memberNewsMonthlyScheduler := buildMemberNewsComponents(memberNewsService, formatter, deliveryModule.Locker, deliveryModule.Repository, logger)

	triggerHandler := sharedserver.NewTriggerHandler(majorEventScheduler, majorEventMonthlyScheduler, memberNewsScheduler, logger)
	httpServer, err := buildLLMSchedulerHTTPServer(ctx, schedulerConfig.Server.Port, logger, triggerHandler, schedulerConfig.Server.APIKey, majorEventRepository, memberNewsService)
	if err != nil {
		return nil, err
	}
	return newLLMSchedulerRuntime(
		schedulerConfig,
		logger,
		deliveryModule,
		majorEventScheduler,
		majorEventMonthlyScheduler,
		majorEventScraperScheduler,
		memberNewsScheduler,
		memberNewsMonthlyScheduler,
		httpServer,
	), nil
}

func newLLMSchedulerRuntime(
	schedulerConfig *config.LLMSchedulerConfig,
	logger *slog.Logger,
	deliveryModule *DeliveryModule,
	majorEventScheduler *mescheduler.Scheduler,
	majorEventMonthlyScheduler *mescheduler.MonthlyScheduler,
	majorEventScraperScheduler *mescraper.RuntimeScheduler,
	memberNewsScheduler *mnscheduler.Scheduler,
	memberNewsMonthlyScheduler *mnscheduler.MonthlyScheduler,
	httpServer *http.Server,
) *LLMSchedulerRuntime {
	return &LLMSchedulerRuntime{
		Config:                     schedulerConfig,
		Logger:                     logger,
		MajorEventScheduler:        majorEventScheduler,
		MajorEventMonthlyScheduler: majorEventMonthlyScheduler,
		MajorEventScraperScheduler: majorEventScraperScheduler,
		MemberNewsScheduler:        memberNewsScheduler,
		MemberNewsMonthlyScheduler: memberNewsMonthlyScheduler,
		httpServer:                 httpServer,
	}
}

func buildMajorEventRepository(
	ctx context.Context,
	postgresService database.Client,
	logger *slog.Logger,
	autoPrepareSchema bool,
) *majorevent.Repository {
	repository := majorevent.NewRepository(postgresService, logger)
	if autoPrepareSchema {
		if createErr := repository.CreateTable(ctx); createErr != nil {
			logger.Error("Failed to create major_event_subscriptions table", slog.String("error", createErr.Error()))
		}
	}
	return repository
}

func buildLLMSchedulerDeliveryModule(
	cacheService cache.Client,
	postgresService database.Client,
	logger *slog.Logger,
) *DeliveryModule {
	return BuildDeliveryModule(cacheService, postgresService, logger)
}

func buildLLMSchedulerHTTPServer(
	ctx context.Context,
	port int,
	logger *slog.Logger,
	triggerHandler *sharedserver.TriggerHandler,
	apiKey string,
	majorEventRepository *majorevent.Repository,
	memberNewsService *membernews.Service,
) (*http.Server, error) {
	if strings.TrimSpace(apiKey) == "" && (triggerHandler != nil || majorEventRepository != nil || memberNewsService != nil) {
		return nil, fmt.Errorf("build llm scheduler router: API_SECRET_KEY required")
	}

	router, err := buildTriggerRouter(ctx, logger, triggerHandler, apiKey)
	if err != nil {
		return nil, fmt.Errorf("build llm scheduler router: %w", err)
	}

	//nolint:contextcheck // gin handlers use per-request context via c.Request.Context()
	registerMajorEventInternalRoutes(router, apiKey, majorEventRepository)
	//nolint:contextcheck // gin handlers use per-request context via c.Request.Context()
	registerMemberNewsInternalRoutes(router, apiKey, memberNewsService)

	addr := fmt.Sprintf(":%d", port)
	return sharedserver.NewH2CServer(addr, router, "hololive-llm-sched.http"), nil
}
