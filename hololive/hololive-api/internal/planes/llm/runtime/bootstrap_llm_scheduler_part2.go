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

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent"
	mescheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scheduler"
	mescraper "github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent/scraper"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"
	mnscheduler "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/scheduler"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

func buildLLMSchedulerReadyProbe(postgresService database.Client, cacheService cache.Client) *sharedreadiness.Probe {
	return sharedreadiness.NewProbe("llm",
		sharedreadiness.PostgresCheck(postgresService),
		sharedreadiness.ValkeyCheck(cacheService),
	)
}

func newLLMSchedulerRuntime(
	schedulerConfig *settings.LLMSchedulerConfig,
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
	triggerHandler *sharedserver.TriggerHandler,
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
	triggerHandler *sharedserver.TriggerHandler,
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
