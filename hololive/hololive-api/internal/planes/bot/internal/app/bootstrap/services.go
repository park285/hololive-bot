package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
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

	irisClient, err := providers.ProvideIrisClient( //nolint:contextcheck,nolintlint // guard 콜백(func(net.IP) error)에 dial ctx 미전달로 per-dial DNS가 자체 ctx를 root함; 도달 가능한 build ctx는 bounded라 관통 시 프로덕션 장애. workspace-wide 분석 전용 call-graph false positive.
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
	messageStrings := messagestrings.NewStore(infra.Postgres.GetPool(), logger)
	if err := messageStrings.Load(ctx); err != nil {
		logger.WarnContext(ctx, "message_strings 초기 적재 실패, lazy 재시도로 진행", "error", err)
	}
	messageAdapter := adapter.NewMessageAdapter(appConfig.Bot.Prefix, appConfig.Bot.MentionPrefix)
	formatter := adapter.NewResponseFormatter(appConfig.Bot.Prefix, templateRenderer, adapter.WithMessageStrings(messageStrings), adapter.WithSeeMoreFold(appConfig.Bot.SeeMoreFold))

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
		appConfig, infra, foundation, alarmYouTubeStack, integrationServices, messageAdapter, formatter, messageStrings, irisClient, logger,
	)

	return &BotInfrastructure{
		Deps:           deps,
		AlarmCRUD:      alarmYouTubeStack.AlarmMode.AlarmCRUD,
		HolodexService: foundation.HolodexService,
		Postgres:       infra.Postgres,
		Cache:          infra.Cache,
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
	messageStrings *messagestrings.Store,
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
		messageStrings,
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
