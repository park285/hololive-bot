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

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	"github.com/kapu/hololive-kakao-bot-go/internal/server"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/system"
	triggerclient "github.com/kapu/hololive-kakao-bot-go/internal/service/trigger"
)

func buildAdminServerDependencies(
	ctx context.Context,
	cfg *config.Config,
	infra *appbootstrap.AdminAPIInfrastructure,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) (*appbootstrap.AdminServerDependencies, error) {
	if cfg == nil {
		return nil, errors.New("build admin server dependencies: config is nil")
	}

	if infra == nil || infra.Cache == nil || infra.Postgres == nil {
		return nil, errors.New("build admin server dependencies: admin infrastructure is incomplete")
	}

	authService, err := buildAdminAuthService(ctx, cfg, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin server dependencies: %w", err)
	}

	settingsComponents := buildAdminSettingsComponents(cfg, infra, scraperScheduler, logger)
	systemCollector := buildAdminSystemCollector(cfg)
	domainHandlers := buildAdminAPIHandlers(
		infra,
		scraperScheduler,
		settingsComponents.settingsApplier,
		settingsComponents.majorEventTriggerClient,
		systemCollector,
		logger,
	)

	return &appbootstrap.AdminServerDependencies{
		DomainHandlers: domainHandlers,
		AuthHandler:    server.NewAuthHandler(authService, logger),
		Cache:          infra.Cache,
	}, nil
}

type adminSettingsComponents struct {
	settingsApplier         sharedsettings.SettingsApplier
	majorEventTriggerClient *triggerclient.Client
}

func buildAdminAuthService(
	ctx context.Context,
	cfg *config.Config,
	infra *appbootstrap.AdminAPIInfrastructure,
	logger *slog.Logger,
) (*authsvc.Service, error) {
	authCfg := authsvc.DefaultConfig()
	authCfg.AutoPrepareSchema = cfg.Postgres.AutoPrepareSchema

	authService, err := authsvc.NewService(
		ctx,
		infra.Postgres.GetGormDB(),
		infra.Cache,
		logger,
		authCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("create auth service: %w", err)
	}

	return authService, nil
}

func buildAdminSettingsComponents(
	cfg *config.Config,
	infra *appbootstrap.AdminAPIInfrastructure,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) adminSettingsComponents {
	localSettingsApplier := sharedsettings.NewLocalSettingsApplier(
		infra.YouTubeService,
		infra.HolodexService,
		scraperScheduler,
		infra.AlarmCRUD,
	)
	settingsApplier := newBotSettingsApplier(localSettingsApplier, nil, logger)

	var majorEventTriggerClient *triggerclient.Client

	if strings.TrimSpace(cfg.LLMSchedulerURL) != "" {
		majorEventTriggerClient = triggerclient.NewClient(cfg.LLMSchedulerURL, cfg.Server.APIKey, logger)
		settingsApplier = newBotSettingsApplier(localSettingsApplier, majorEventTriggerClient, logger)
	} else {
		logger.Warn("LLM scheduler URL not configured; trigger routes and membernews run-now are disabled",
			slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"),
		)
	}

	return adminSettingsComponents{
		settingsApplier:         settingsApplier,
		majorEventTriggerClient: majorEventTriggerClient,
	}
}

func buildAdminSystemCollector(cfg *config.Config) *system.Collector {
	return system.NewCollector(
		[]system.ServiceEndpoint{
			{Name: "llm-scheduler", URL: cfg.Services.LLMSchedulerHealthURL},
			{Name: "twentyq", URL: cfg.Services.GameBotTwentyQHealthURL},
			{Name: "turtlesoup", URL: cfg.Services.GameBotTurtleHealthURL},
		},
	)
}

func buildAdminAPIHandlers(
	infra *appbootstrap.AdminAPIInfrastructure,
	scraperScheduler *poller.Scheduler,
	settingsApplier sharedsettings.SettingsApplier,
	majorEventTriggerClient *triggerclient.Client,
	systemCollector *system.Collector,
	logger *slog.Logger,
) *server.DomainAPIHandlers {
	var communityShortsOpsRepo server.YouTubeCommunityShortsOpsRepository
	if infra.Postgres != nil && infra.Postgres.GetGormDB() != nil {
		communityShortsOpsRepo = outbox.NewDeliveryTelemetryRepository(infra.Postgres.GetGormDB())
	}

	return server.NewAPIHandler(
		infra.MemberRepo,
		infra.MemberCache,
		infra.Cache,
		infra.Profiles,
		infra.AlarmCRUD,
		infra.HolodexService,
		infra.YouTubeService,
		scraperScheduler,
		infra.StatsRepo,
		communityShortsOpsRepo,
		infra.ActivityLogger,
		infra.SettingsService,
		settingsApplier,
		infra.ACLService,
		systemCollector,
		infra.TemplateAdminSvc,
		majorEventTriggerClient,
		majorEventTriggerClient,
		logger,
	).DomainHandlers()
}
