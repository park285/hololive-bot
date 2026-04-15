package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/template"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

func InitCoreInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *CoreInfrastructure, retErr error) {
	infra, err := InitInfraResources(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	irisClient, err := providers.ProvideIrisClient(logger)
	if err != nil {
		infra.Cleanup()
		return nil, err
	}

	defer func() {
		if retErr != nil {
			infra.Cleanup()
		}
	}()

	templateRenderer := template.NewRenderer(infra.Postgres.GetGormDB(), logger)
	messageAdapter := adapter.NewMessageAdapter(cfg.Bot.Prefix, cfg.Bot.MentionPrefix)
	formatter := adapter.NewResponseFormatter(cfg.Bot.Prefix, templateRenderer)

	streamFoundation, err := InitScraperHolodexProfileFoundation(ctx, cfg, infra, logger)
	if err != nil {
		return nil, err
	}

	alarmYouTubeStack, err := InitAlarmYouTubeStack(ctx, cfg, infra, streamFoundation, irisClient, formatter, logger)
	if err != nil {
		return nil, err
	}

	integrationServices, err := InitCoreIntegrationServices(ctx, cfg, infra, logger)
	if err != nil {
		return nil, err
	}

	modules := BuildBotDependencyModules(
		cfg,
		infra,
		alarmYouTubeStack.AlarmMode,
		streamFoundation.HolodexService,
		messageAdapter,
		formatter,
		irisClient,
		streamFoundation.ProfileService,
		alarmYouTubeStack.MemberMatcher,
		alarmYouTubeStack.YouTubeStack,
		alarmYouTubeStack.ActivityLogger,
		alarmYouTubeStack.SettingsService,
		integrationServices.ACLService,
		integrationServices.MajorEventRepo,
		integrationServices.MemberNewsService,
		integrationServices.CommandBuilders,
		integrationServices.WorkerPool,
		logger,
	)
	deps := ProvideBotDependencies(modules)

	return &CoreInfrastructure{
		Deps:             deps,
		AlarmService:     alarmYouTubeStack.AlarmMode.AlarmService,
		AlarmCRUD:        alarmYouTubeStack.AlarmMode.AlarmCRUD,
		HolodexService:   streamFoundation.HolodexService,
		YTStack:          alarmYouTubeStack.YouTubeStack,
		PhotoSync:        holodex.NewPhotoSyncService(streamFoundation.HolodexService, infra.MemberRepo, logger),
		TemplateRenderer: templateRenderer,
		TemplateAdminSvc: BuildTemplateAdminService(infra, templateRenderer, logger),
		SharedRL:         streamFoundation.SharedRL,
		Cleanup:          infra.Cleanup,
	}, nil
}
