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
//
//nolint:funlen // bootstrap wiring; keep the dependency graph visible in one place
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

	foundation, err := initScraperHolodexProfileFoundation(ctx, cfg, infra, logger)
	if err != nil {
		return nil, err
	}

	alarmAndStack, err := initAlarmYouTubeStack(
		ctx,
		cfg,
		infra,
		foundation,
		irisClient,
		formatter,
		logger,
	)
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
		alarmAndStack.alarmMode,
		foundation.holodexService,
		messageAdapter,
		formatter,
		irisClient,
		foundation.profileService,
		alarmAndStack.memberMatcher,
		alarmAndStack.youTubeStack,
		alarmAndStack.activityLogger,
		alarmAndStack.settingsService,
		integrationServices.aclService,
		integrationServices.majorEventRepo,
		integrationServices.memberNewsService,
		integrationServices.workerPool,
		logger,
	)
	deps := ProvideBotDependencies(modules)

	return &coreInfrastructure{
		deps:                         deps,
		alarmService:                 alarmAndStack.alarmMode.alarmService,
		alarmCRUD:                    alarmAndStack.alarmMode.alarmCRUD,
		holodexService:               foundation.holodexService,
		ytStack:                      alarmAndStack.youTubeStack,
		photoSync:                    holodex.NewPhotoSyncService(foundation.holodexService, infra.memberRepo, logger),
		templateRenderer:             templateRenderer,
		templateAdminSvc:             buildTemplateAdminService(infra, templateRenderer, logger),
		sharedRL:                     foundation.sharedRL,
		runtimeAlarmSchedulerBuilder: defaultRuntimeAlarmSchedulerBuilder,
		cleanupCache:                 infra.cleanupCache,
		cleanupDB:                    infra.cleanupDB,
	}, nil
}
