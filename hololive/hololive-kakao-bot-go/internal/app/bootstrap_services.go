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
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/template"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

// initCoreInfrastructure 는 공통 인프라를 초기화한다.
func initCoreInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *coreInfrastructure, retErr error) {
	irisClient := providers.ProvideIrisClient(cfg.Iris, logger)

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

	templateRenderer := template.NewRenderer(infra.postgresService.GetGormDB(), logger)
	messageAdapter := adapter.NewMessageAdapter(cfg.Bot.Prefix)
	formatter := adapter.NewResponseFormatter(cfg.Bot.Prefix, templateRenderer)

	streamFoundation, err := initScraperHolodexProfileFoundation(ctx, cfg, infra, logger)
	if err != nil {
		return nil, err
	}

	alarmYouTubeStack, err := initAlarmYouTubeStack(ctx, cfg, infra, streamFoundation, irisClient, formatter, logger)
	if err != nil {
		return nil, err
	}

	integrationServices, err := initCoreIntegrationServices(ctx, cfg, infra, logger)
	if err != nil {
		return nil, err
	}

	modules := buildBotDependencyModules(
		cfg,
		infra,
		alarmYouTubeStack.alarmMode,
		streamFoundation.holodexService,
		messageAdapter,
		formatter,
		irisClient,
		streamFoundation.profileService,
		alarmYouTubeStack.memberMatcher,
		alarmYouTubeStack.youTubeStack,
		alarmYouTubeStack.activityLogger,
		alarmYouTubeStack.settingsService,
		integrationServices.aclService,
		integrationServices.majorEventRepo,
		integrationServices.memberNewsService,
		integrationServices.workerPool,
		logger,
	)
	deps := ProvideBotDependencies(modules)

	return &coreInfrastructure{
		deps:             deps,
		alarmService:     alarmYouTubeStack.alarmMode.alarmService,
		alarmCRUD:        alarmYouTubeStack.alarmMode.alarmCRUD,
		holodexService:   streamFoundation.holodexService,
		ytStack:          alarmYouTubeStack.youTubeStack,
		photoSync:        holodex.NewPhotoSyncService(streamFoundation.holodexService, infra.memberRepo, logger),
		templateRenderer: templateRenderer,
		templateAdminSvc: buildTemplateAdminService(infra, templateRenderer, logger),
		sharedRL:         streamFoundation.sharedRL,
		cleanupCache:     infra.cleanupCache,
		cleanupDB:        infra.cleanupDB,
	}, nil
}
