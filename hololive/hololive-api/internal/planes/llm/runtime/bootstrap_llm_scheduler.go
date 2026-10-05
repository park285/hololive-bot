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
	"strings"
	"sync"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	apiserver "github.com/kapu/hololive-api/internal/httpapi"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent"
	mescheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scheduler"
	mescraper "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scraper"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	mnscheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/scheduler"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	cacheproviders "github.com/kapu/hololive-shared/pkg/providers/cache"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	memberproviders "github.com/kapu/hololive-shared/pkg/providers/member"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

//nolint:gosec // 이 값은 자격 증명이 아니라 고정된 구성 오류 메시지다.
const llmSchedulerAPISecretRequired = "build llm scheduler router: API_SECRET_KEY required"

type LLMSchedulerRuntime struct {
	lifecycle.Managed

	Config *apiconfig.LLMSchedulerConfig
	Logger *slog.Logger

	MajorEventScheduler        *mescheduler.Scheduler
	MajorEventMonthlyScheduler *mescheduler.MonthlyScheduler
	MajorEventScraperScheduler *mescraper.RuntimeScheduler
	MemberNewsScheduler        *mnscheduler.Scheduler
	MemberNewsMonthlyScheduler *mnscheduler.MonthlyScheduler

	httpServers *sharedserver.RuntimeHTTPServers
	memberCache *member.Cache

	lifecycleMu        sync.Mutex
	started            bool
	stopping           bool
	tasksCancel        context.CancelFunc
	schedulersStopOnce sync.Once
	schedulersDone     chan struct{}
	httpStopInit       sync.Once
	httpStopGate       chan struct{}
	httpStopErr        error
	httpQuiesced       bool
	drainErr           error
	resourcesCloseOnce sync.Once
	resourcesDone      chan struct{}
	resourcesErr       error
}

func (r *LLMSchedulerRuntime) startHTTPServer(errCh chan<- error) {
	if r.httpServers == nil {
		return
	}

	r.httpServers.Start(r.Logger, errCh)
	r.Logger.Info("LLM scheduler HTTP server started",
		slog.String("addr", r.httpServers.Addr()))
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

func BuildLLMSchedulerRuntime(ctx context.Context, schedulerConfig *apiconfig.LLMSchedulerConfig, logger *slog.Logger) (*LLMSchedulerRuntime, error) {
	if schedulerConfig == nil {
		return nil, errors.New("llm scheduler config must not be nil")
	}

	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}

	cacheResources, cleanupCache, err := cacheproviders.ProvideCacheResources(ctx, schedulerConfig.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("init cache: %w", err)
	}

	cacheService := cacheResources.Service

	databaseResources, cleanupDB, err := databaseproviders.ProvideDatabaseResources(ctx, &schedulerConfig.Postgres, logger)
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

		return nil, fmt.Errorf("build LLM scheduler components: %w", err)
	}

	runtime.Managed = lifecycle.NewManaged(func() {
		runtime.memberCache.Close()
		cleanup()
	})

	return runtime, nil
}

func buildLLMSchedulerComponents(
	ctx context.Context,
	schedulerConfig *apiconfig.LLMSchedulerConfig,
	logger *slog.Logger,
	cacheService cache.Client,
	postgresService database.Client,
) (_ *LLMSchedulerRuntime, err error) {
	guards, err := buildLLMGuards(logger)
	if err != nil {
		return nil, fmt.Errorf("build LLM guards: %w", err)
	}

	memberRepository := member.NewMemberRepository(postgresService, logger)

	memberCache, err := memberproviders.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("init member cache: %w", err)
	}

	defer func() {
		if err != nil {
			memberCache.Close()
		}
	}()

	memberServiceAdapter := member.NewMemberServiceAdapter(memberCache)
	memberDataProvider := memberServiceAdapter

	templateRenderer := template.NewRenderer(postgresService.GetPool(), logger)
	formatter := newLLMSchedulerFormatter(schedulerConfig.Bot.Prefix, templateRenderer, logger, schedulerConfig.Bot.SeeMoreFold)

	formatter.store, err = loadLLMMessageStrings(ctx, postgresService, logger)
	if err != nil {
		return nil, fmt.Errorf("load llm message strings: %w", err)
	}

	majorEventRepository := buildMajorEventRepository(postgresService, logger)

	memberNewsService, err := initMemberNewsService(ctx, schedulerConfig.SelectedLLMProvider(), &schedulerConfig.LLM, schedulerConfig.Exa, schedulerConfig.MemberNewsXAllowlistPath, postgresService, memberDataProvider, guards, logger)
	if err != nil {
		return nil, fmt.Errorf("init member news service: %w", err)
	}

	deliveryModule, err := buildLLMSchedulerDeliveryModule(cacheService, postgresService, logger)
	if err != nil {
		return nil, fmt.Errorf("build delivery module: %w", err)
	}

	out, err := buildLLMSchedulerRuntimeComponents(
		ctx,
		schedulerConfig,
		logger,
		postgresService,
		cacheService,
		majorEventRepository,
		memberNewsService,
		formatter,
		deliveryModule,
		guards,
	)
	if err != nil {
		return nil, fmt.Errorf("build LLM scheduler runtime components: %w", err)
	}

	out.memberCache = memberCache

	return out, nil
}

func buildLLMSchedulerRuntimeComponents(
	ctx context.Context,
	schedulerConfig *apiconfig.LLMSchedulerConfig,
	logger *slog.Logger,
	postgresService database.Client,
	cacheService cache.Client,
	majorEventRepository *majorevent.Repository,
	memberNewsService *membernews.Service,
	formatter *llmSchedulerFormatter,
	deliveryModule *DeliveryModule,
	guards *llmGuards,
) (*LLMSchedulerRuntime, error) {
	summarizer := buildMajorEventSummarizer(schedulerConfig, cacheService, guards, logger)
	majorEventScheduler, majorEventMonthlyScheduler, majorEventScraperScheduler := buildMajorEventComponents(
		majorEventRepository,
		formatter,
		summarizer,
		deliveryModule.Locker,
		deliveryModule.Repository,
		guards,
		logger,
	)
	memberNewsScheduler, memberNewsMonthlyScheduler := buildMemberNewsComponents(memberNewsService, formatter, deliveryModule.Locker, deliveryModule.Repository, guards.output, logger)

	triggerHandler := apiserver.NewTriggerHandler(majorEventScheduler, majorEventMonthlyScheduler, memberNewsScheduler, logger)
	readyProbe := buildLLMSchedulerReadyProbe(postgresService, cacheService)

	httpServers, err := buildLLMSchedulerHTTPServers(ctx, &schedulerConfig.Server, logger, triggerHandler, schedulerConfig.Server.APIKey, majorEventRepository, memberNewsService, readyProbe)
	if err != nil {
		return nil, fmt.Errorf("build LLM scheduler HTTP servers: %w", err)
	}

	return newLLMSchedulerRuntime(
		schedulerConfig,
		logger,
		majorEventScheduler,
		majorEventMonthlyScheduler,
		majorEventScraperScheduler,
		memberNewsScheduler,
		memberNewsMonthlyScheduler,
		httpServers,
	), nil
}

func buildLLMSchedulerReadyProbe(postgresService database.Client, cacheService cache.Client) *sharedreadiness.Probe {
	return sharedreadiness.NewProbe("llm",
		sharedreadiness.PostgresCheck(postgresService),
		sharedreadiness.ValkeyCheck(cacheService),
	)
}

func newLLMSchedulerRuntime(
	schedulerConfig *apiconfig.LLMSchedulerConfig,
	logger *slog.Logger,
	majorEventScheduler *mescheduler.Scheduler,
	majorEventMonthlyScheduler *mescheduler.MonthlyScheduler,
	majorEventScraperScheduler *mescraper.RuntimeScheduler,
	memberNewsScheduler *mnscheduler.Scheduler,
	memberNewsMonthlyScheduler *mnscheduler.MonthlyScheduler,
	httpServers *sharedserver.RuntimeHTTPServers,
) *LLMSchedulerRuntime {
	return &LLMSchedulerRuntime{
		Config:                     schedulerConfig,
		Logger:                     logger,
		MajorEventScheduler:        majorEventScheduler,
		MajorEventMonthlyScheduler: majorEventMonthlyScheduler,
		MajorEventScraperScheduler: majorEventScraperScheduler,
		MemberNewsScheduler:        memberNewsScheduler,
		MemberNewsMonthlyScheduler: memberNewsMonthlyScheduler,
		httpServers:                httpServers,
	}
}

func buildMajorEventRepository(
	postgresService database.Client,
	logger *slog.Logger,
) *majorevent.Repository {
	return majorevent.NewRepository(postgresService, logger)
}

func buildLLMSchedulerDeliveryModule(
	cacheService cache.Client,
	postgresService database.Client,
	logger *slog.Logger,
) (*DeliveryModule, error) {
	out, err := BuildDeliveryModule(cacheService, postgresService, logger)
	if err != nil {
		return nil, fmt.Errorf("build delivery module: %w", err)
	}

	return out, nil
}

func buildLLMSchedulerHTTPServer(
	ctx context.Context,
	port int,
	logger *slog.Logger,
	triggerHandler *apiserver.TriggerHandler,
	apiKey string,
	majorEventRepository *majorevent.Repository,
	memberNewsService *membernews.Service,
) (*http.Server, error) {
	if strings.TrimSpace(apiKey) == "" && (triggerHandler != nil || majorEventRepository != nil || memberNewsService != nil) {
		return nil, errors.New(llmSchedulerAPISecretRequired)
	}

	router, err := buildTriggerRouter(ctx, logger, triggerHandler, apiKey)
	if err != nil {
		return nil, fmt.Errorf("build llm scheduler router: %w", err)
	}

	//nolint:contextcheck // gin handlers use per-request context via c.Request.Context()
	registerMajorEventInternalRoutes(router, httputil.AdminAuthConfig{APIKey: apiKey}, majorEventRepository)
	//nolint:contextcheck // gin handlers use per-request context via c.Request.Context()
	registerMemberNewsInternalRoutes(router, httputil.AdminAuthConfig{APIKey: apiKey}, memberNewsService)

	addr := fmt.Sprintf(":%d", port)

	return sharedserver.NewHTTPServer(addr, router, "hololive-llm-sched.http", sharedserver.LocalPlaneTraceFilter), nil
}

func buildLLMSchedulerHTTPServers(
	ctx context.Context,
	serverConfig *settings.ServerConfig,
	logger *slog.Logger,
	triggerHandler *apiserver.TriggerHandler,
	apiKey string,
	majorEventRepository *majorevent.Repository,
	memberNewsService *membernews.Service,
	readyProbe *sharedreadiness.Probe,
) (*sharedserver.RuntimeHTTPServers, error) {
	if serverConfig == nil {
		serverConfig = &settings.ServerConfig{}
	}

	if strings.TrimSpace(apiKey) == "" && (triggerHandler != nil || majorEventRepository != nil || memberNewsService != nil) {
		return nil, errors.New(llmSchedulerAPISecretRequired)
	}

	router, err := buildTriggerRouter(ctx, logger, triggerHandler, apiKey, readyProbe)
	if err != nil {
		return nil, fmt.Errorf("build llm scheduler router: %w", err)
	}

	//nolint:contextcheck // gin handlers use per-request context via c.Request.Context()
	registerMajorEventInternalRoutes(router, httputil.AdminAuthConfig{APIKey: apiKey}, majorEventRepository)
	//nolint:contextcheck // gin handlers use per-request context via c.Request.Context()
	registerMemberNewsInternalRoutes(router, httputil.AdminAuthConfig{APIKey: apiKey}, memberNewsService)

	out, err := sharedserver.NewRuntimeHTTPServers(ctx, serverConfig, router, "hololive-llm-sched.http",
		nil, sharedserver.LocalPlaneTraceFilter)
	if err != nil {
		return nil, fmt.Errorf("runtime HTTP servers: %w", err)
	}

	return out, nil
}

// loadLLMMessageStrings는 llm plane formatter가 쓰는 message_strings를 기동 때 적재하고 뉴스 분류 namespace를
// 검증한다. 실패하면 기동을 실패시킨다(DEC-20260926-hololive-message-strings-startup-validation).
func loadLLMMessageStrings(ctx context.Context, postgresService database.Client, logger *slog.Logger) (*messagestrings.Store, error) {
	store := messagestrings.NewStore(postgresService.GetPool(), logger)
	if err := store.Load(ctx); err != nil {
		return nil, fmt.Errorf("load message strings: %w", err)
	}

	if err := store.Validate(llmMessageStringRequirements()); err != nil {
		return nil, fmt.Errorf("validate message strings: %w", err)
	}

	return store, nil
}

func llmMessageStringRequirements() messagestrings.Requirements {
	return messagestrings.Requirements{Namespaces: []string{messagestrings.NamespaceNewsCat}}
}
