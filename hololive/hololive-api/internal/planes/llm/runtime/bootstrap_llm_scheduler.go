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
	"sync"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent"
	mescheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scheduler"
	mescraper "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scraper"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	mnscheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/scheduler"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type LLMSchedulerRuntime struct {
	lifecycle.Managed

	Config *apiconfig.LLMSchedulerConfig
	Logger *slog.Logger

	MajorEventScheduler        *mescheduler.Scheduler
	MajorEventMonthlyScheduler *mescheduler.MonthlyScheduler
	MajorEventScraperScheduler *mescraper.RuntimeScheduler
	MemberNewsScheduler        *mnscheduler.Scheduler
	MemberNewsMonthlyScheduler *mnscheduler.MonthlyScheduler

	// MajorEventSubscriptions·MemberNews는 bot plane이 같은 프로세스에서 직접 호출하는 구독 저장소와
	// digest 서비스다. 수명은 llm plane이 빌린 PostgreSQL pool을 따른다.
	MajorEventSubscriptions *majorevent.Repository
	MemberNews              *membernews.Service

	httpServers *sharedserver.RuntimeHTTPServers

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

func BuildLLMSchedulerRuntime(ctx context.Context, schedulerConfig *apiconfig.LLMSchedulerConfig, infra *sharedmodules.InfraModule, logger *slog.Logger) (*LLMSchedulerRuntime, error) {
	if schedulerConfig == nil {
		return nil, errors.New("llm scheduler config must not be nil")
	}

	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}

	if infra == nil {
		return nil, errors.New("shared infra must not be nil")
	}

	runtime, err := buildLLMSchedulerComponents(ctx, schedulerConfig, logger, infra.Cache, infra.Postgres, infra.MemberCache)
	if err != nil {
		return nil, fmt.Errorf("build LLM scheduler components: %w", err)
	}

	return runtime, nil
}

// llmSchedulerCache는 LLM scheduler 조립이 cache에 요구하는 작업의 합집합이다. 행사 요약 cache(Get·Set),
// digest 실행 lock(SetNX·CompareAndDelete), readiness(IsConnected)를 쓴다.
type llmSchedulerCache interface {
	cache.KeyValueReader
	cache.KeyValueWriter
	cache.ScriptCache
	sharedreadiness.ConnectionChecker
}

func buildLLMSchedulerComponents(
	ctx context.Context,
	schedulerConfig *apiconfig.LLMSchedulerConfig,
	logger *slog.Logger,
	cacheService llmSchedulerCache,
	postgresService database.Client,
	memberCache *member.Cache,
) (_ *LLMSchedulerRuntime, err error) {
	guards, err := buildLLMGuards(logger)
	if err != nil {
		return nil, fmt.Errorf("build LLM guards: %w", err)
	}

	memberDataProvider := memberCache

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

	return out, nil
}

func buildLLMSchedulerRuntimeComponents(
	ctx context.Context,
	schedulerConfig *apiconfig.LLMSchedulerConfig,
	logger *slog.Logger,
	postgresService database.Client,
	cacheService llmSchedulerCache,
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

	readyProbe := buildLLMSchedulerReadyProbe(postgresService, cacheService)

	httpServers, err := buildLLMSchedulerHTTPServers(ctx, &schedulerConfig.Server, logger, readyProbe)
	if err != nil {
		return nil, fmt.Errorf("build LLM scheduler HTTP servers: %w", err)
	}

	runtime := newLLMSchedulerRuntime(
		schedulerConfig,
		logger,
		majorEventScheduler,
		majorEventMonthlyScheduler,
		majorEventScraperScheduler,
		memberNewsScheduler,
		memberNewsMonthlyScheduler,
		httpServers,
	)

	runtime.MajorEventSubscriptions = majorEventRepository
	runtime.MemberNews = memberNewsService

	return runtime, nil
}

func buildLLMSchedulerReadyProbe(postgresService sharedreadiness.Pinger, cacheService sharedreadiness.ConnectionChecker) *sharedreadiness.Probe {
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
	cacheService deliveryLockCache,
	postgresService database.Client,
	logger *slog.Logger,
) (*DeliveryModule, error) {
	out, err := BuildDeliveryModule(cacheService, postgresService, logger)
	if err != nil {
		return nil, fmt.Errorf("build delivery module: %w", err)
	}

	return out, nil
}

func buildLLMSchedulerHTTPServers(
	ctx context.Context,
	serverConfig *settings.ServerConfig,
	logger *slog.Logger,
	readyProbe *sharedreadiness.Probe,
) (*sharedserver.RuntimeHTTPServers, error) {
	if serverConfig == nil {
		serverConfig = &settings.ServerConfig{}
	}

	router, err := buildLLMSchedulerRouter(ctx, logger, serverConfig.APIKey, readyProbe)
	if err != nil {
		return nil, err
	}

	out, err := sharedserver.NewRuntimeHTTPServers(ctx, serverConfig, router, "hololive-llm-sched.http",
		nil, sharedserver.LocalPlaneTraceFilter)
	if err != nil {
		return nil, fmt.Errorf("runtime HTTP servers: %w", err)
	}

	return out, nil
}

// loadLLMMessageStrings는 llm plane formatter가 쓰는 message_strings를 기동 때 적재하고 뉴스 분류 namespace를
// 검증한다. 실패하면 기동을 실패시킨다.
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
