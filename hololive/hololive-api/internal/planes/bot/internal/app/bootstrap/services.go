package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot"
)

func InitBotInfrastructure(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (_ *BotInfrastructure, retErr error) {
	infra, err := InitInfraResources(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	irisClient, err := providers.ProvideIrisClient(
		logger,
		iris.WithBaseURL(appConfig.Iris.BaseURL),
		iris.WithBotToken(appConfig.Iris.BotToken),
	)
	if err != nil {
		infra.Cleanup()
		return nil, err
	}

	defer func() {
		if retErr != nil {
			closeIrisClientForCleanup(irisClient, logger)
			infra.Cleanup()
		}
	}()

	templateRenderer := template.NewRenderer(infra.Postgres.GetPool(), logger)
	messageAdapter := adapter.NewMessageAdapter(appConfig.Bot.Prefix, appConfig.Bot.MentionPrefix)
	formatter := adapter.NewResponseFormatter(appConfig.Bot.Prefix, templateRenderer)

	foundation, err := InitScraperHolodexProfileFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, err
	}

	alarmYouTubeStack, err := InitAlarmYouTubeStack(ctx, appConfig, infra, foundation, irisClient, formatter, logger)
	if err != nil {
		return nil, err
	}

	integrationServices, err := InitCoreIntegrationServices(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, err
	}

	deps := provideBotDependenciesFromStacks(
		appConfig, infra, foundation, alarmYouTubeStack, integrationServices, messageAdapter, formatter, irisClient, logger,
	)

	return &BotInfrastructure{
		Deps:           deps,
		AlarmCRUD:      alarmYouTubeStack.AlarmMode.AlarmCRUD,
		HolodexService: foundation.HolodexService,
		Cleanup:        composeBotInfrastructureCleanup(infra.Cleanup, irisClient, logger),
	}, nil
}

func provideBotDependenciesFromStacks(
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	foundation *ScraperHolodexProfileFoundation,
	alarmYouTubeStack *AlarmYouTubeStackComponents,
	integrationServices *CoreIntegrationServices,
	messageAdapter *adapter.MessageAdapter,
	formatter *adapter.ResponseFormatter,
	irisClient iris.BotClient,
	logger *slog.Logger,
) *bot.Dependencies {
	modules := BuildBotDependencyModules(
		appConfig,
		infra,
		alarmYouTubeStack.AlarmMode,
		foundation.HolodexService,
		messageAdapter,
		formatter,
		irisClient,
		foundation.ProfileService,
		alarmYouTubeStack.Matcher,
		alarmYouTubeStack.YouTubeStack,
		alarmYouTubeStack.ActivityLogger,
		alarmYouTubeStack.SettingsService,
		integrationServices.ACLService,
		integrationServices.MajorEventRepository,
		integrationServices.MemberNewsService,
		integrationServices.CommandBuilders,
		integrationServices.WorkerPool,
		logger,
	)

	return ProvideBotDependencies(&modules)
}
