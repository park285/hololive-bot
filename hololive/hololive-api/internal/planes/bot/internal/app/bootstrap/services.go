package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func InitBotInfrastructure(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (_ *BotInfrastructure, retErr error) {
	infra, err := InitInfraResources(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("init infra resources: %w", err)
	}

	irisClient, err := providers.ProvideIrisClient(
		&appConfig.Iris,
		logger,
		iris.WithBaseURL(appConfig.Iris.BaseURL),
		iris.WithBotToken(appConfig.Iris.BotToken),
	)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("provide iris client: %w", err)
	}

	defer func() {
		retErr = cleanupFailedBotInfrastructureBuild(retErr, irisClient, infra, logger)
	}()

	infrastructure, err := buildBotInfrastructureServices(ctx, appConfig, logger, infra, irisClient)
	if err != nil {
		return nil, fmt.Errorf("build bot infrastructure services: %w", err)
	}

	return infrastructure, nil
}

func cleanupFailedBotInfrastructureBuild(
	buildErr error,
	irisClient providers.ManagedIrisClient,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) error {
	if buildErr == nil {
		return nil
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
	irisClient providers.ManagedIrisClient,
) (*BotInfrastructure, error) {
	templateRenderer := template.NewRenderer(infra.Postgres.GetPool(), logger)

	messageStrings, err := loadBotMessageStrings(ctx, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("load bot message strings: %w", err)
	}

	messageAdapter := messaging.NewMessageAdapter(appConfig.Bot.Prefix, appConfig.Bot.MentionPrefix)
	formatter := messageformatter.NewResponseFormatter(appConfig.Bot.Prefix, templateRenderer, messageformatter.WithMessageStrings(messageStrings), messageformatter.WithSeeMoreFold(appConfig.Bot.SeeMoreFold))

	foundation, err := InitScraperHolodexFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("init scraper holodex foundation: %w", err)
	}

	alarmYouTubeStack, err := InitAlarmYouTubeStack(appConfig, foundation, irisClient, formatter, logger)
	if err != nil {
		return nil, fmt.Errorf("init alarm youtube stack: %w", err)
	}

	integrationServices, err := InitCoreIntegrationServices(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("init core integration services: %w", err)
	}

	deps := provideBotDependenciesFromStacks(
		appConfig, infra, foundation, alarmYouTubeStack, integrationServices, messageAdapter, formatter, messageStrings, irisClient, logger,
	)

	return &BotInfrastructure{
		Deps:           deps,
		AlarmCRUD:      alarmYouTubeStack.AlarmMode.AlarmCRUD,
		HolodexService: foundation.HolodexService,
		IrisRoomLister: irisClient,
		Postgres:       infra.Postgres,
		Cache:          infra.Cache,
		Cleanup: composeBotInfrastructureCleanup(
			infra.Cleanup,
			irisClient,
			append([]io.Closer{alarmYouTubeStack.AlarmMode.AlarmClient}, integrationServices.SchedulerTransports...),
			logger,
		),
	}, nil
}

func provideBotDependenciesFromStacks(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	foundation *ScraperHolodexFoundation,
	alarmYouTubeStack *AlarmYouTubeStackComponents,
	integrationServices *CoreIntegrationServices,
	messageAdapter *messaging.MessageAdapter,
	formatter *messageformatter.ResponseFormatter,
	messageStrings *messagestrings.Store,
	irisClient orchestration.BotIrisClient,
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

// loadBotMessageStrings는 bot plane이 쓰는 message_strings를 기동 때 한 번 적재하고 formatter·오류 응답·기념일 카드
// key를 검증한다. 실패하면 기동을 실패시킨다. 운영 중 재적재와 코드 대체 문구는 없다
// (DEC-20260926-hololive-message-strings-startup-validation).
func loadBotMessageStrings(ctx context.Context, infra *sharedmodules.InfraModule, logger *slog.Logger) (*messagestrings.Store, error) {
	messageStrings := messagestrings.NewStore(infra.Postgres.GetPool(), logger)
	if err := messageStrings.Load(ctx); err != nil {
		return nil, fmt.Errorf("load message strings: %w", err)
	}

	if err := messageStrings.Validate(botMessageStringRequirements()); err != nil {
		return nil, fmt.Errorf("validate message strings: %w", err)
	}

	return messageStrings, nil
}

func botMessageStringRequirements() messagestrings.Requirements {
	requirements := messageformatter.RequiredMessageStrings()

	requirements.Keys = append(requirements.Keys, messagestrings.CalendarKeys()...)

	for _, key := range messaging.ErrorMessageKeys() {
		requirements.Keys = append(requirements.Keys, messagestrings.Key{Namespace: messagestrings.NamespaceError, Name: key})
	}

	return requirements
}
