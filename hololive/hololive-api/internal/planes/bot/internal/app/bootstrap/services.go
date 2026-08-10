package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
)

func InitBotInfrastructure(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (_ *BotInfrastructure, retErr error) {
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

	var ownedAlarmService *alarmservice.AlarmService
	defer func() {
		retErr = cleanupFailedBotInfrastructureBuild(ctx, retErr, ownedAlarmService, irisClient, infra, logger)
	}()

	infrastructure, alarmService, err := buildBotInfrastructureServices(ctx, appConfig, logger, infra, irisClient)
	ownedAlarmService = alarmService
	if err != nil {
		return nil, err
	}
	return infrastructure, nil
}

func cleanupFailedBotInfrastructureBuild(
	ctx context.Context,
	buildErr error,
	ownedAlarmService *alarmservice.AlarmService,
	irisClient iris.Client,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) error {
	if buildErr == nil {
		return nil
	}
	if ownedAlarmService != nil {
		if err := ownedAlarmService.Close(ctx); err != nil {
			buildErr = errors.Join(buildErr, fmt.Errorf("close alarm service after bot infrastructure build failure: %w", err))
		}
	}
	closeIrisClientForCleanup(irisClient, logger)
	infra.Cleanup()
	return buildErr
}

func buildBotInfrastructureServices(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
	infra *sharedmodules.InfraModule,
	irisClient iris.Client,
) (*BotInfrastructure, *alarmservice.AlarmService, error) {
	templateRenderer := template.NewRenderer(infra.Postgres.GetPool(), logger)
	messageStrings := messagestrings.NewStore(infra.Postgres.GetPool(), logger)
	if err := messageStrings.Load(ctx); err != nil {
		logger.WarnContext(ctx, "message_strings 초기 적재 실패, lazy 재시도로 진행", "error", err)
	}
	messageAdapter := messaging.NewMessageAdapter(appConfig.Bot.Prefix, appConfig.Bot.MentionPrefix)
	formatter := messageformatter.NewResponseFormatter(appConfig.Bot.Prefix, templateRenderer, messageformatter.WithMessageStrings(messageStrings), messageformatter.WithSeeMoreFold(appConfig.Bot.SeeMoreFold))

	foundation, err := InitScraperHolodexProfileFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, nil, err
	}

	alarmYouTubeStack, err := InitAlarmYouTubeStack(ctx, appConfig, infra, foundation, irisClient, formatter, logger)
	if err != nil {
		return nil, nil, err
	}
	ownedAlarmService := alarmYouTubeStack.AlarmMode.AlarmService

	integrationServices, err := InitCoreIntegrationServices(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, ownedAlarmService, err
	}

	deps := provideBotDependenciesFromStacks(
		appConfig, infra, foundation, alarmYouTubeStack, integrationServices, messageAdapter, formatter, messageStrings, irisClient, logger,
	)

	return &BotInfrastructure{
		Deps:           deps,
		AlarmCRUD:      alarmYouTubeStack.AlarmMode.AlarmCRUD,
		AlarmService:   ownedAlarmService,
		HolodexService: foundation.HolodexService,
		IrisRoomLister: buildBotIrisRoomLister(irisClient, logger),
		Postgres:       infra.Postgres,
		Cache:          infra.Cache,
		Cleanup:        composeBotInfrastructureCleanup(infra.Cleanup, irisClient, logger),
	}, ownedAlarmService, nil
}

func buildBotIrisRoomLister(irisClient iris.Client, logger *slog.Logger) IrisRoomLister {
	roomLister, ok := irisClient.(IrisRoomLister)
	if !ok {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("bot iris client cannot list joined rooms")
		return nil
	}
	return roomLister
}

func provideBotDependenciesFromStacks(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	foundation *ScraperHolodexProfileFoundation,
	alarmYouTubeStack *AlarmYouTubeStackComponents,
	integrationServices *CoreIntegrationServices,
	messageAdapter *messaging.MessageAdapter,
	formatter *messageformatter.ResponseFormatter,
	messageStrings *messagestrings.Store,
	irisClient iris.BotClient,
	logger *slog.Logger,
) *orchestration.Dependencies {
	modules := BuildBotDependencyModules(
		appConfig,
		infra,
		foundation,
		alarmYouTubeStack,
		integrationServices,
		messageAdapter,
		formatter,
		messageStrings,
		irisClient,
		logger,
	)

	return ProvideBotDependencies(&modules)
}
