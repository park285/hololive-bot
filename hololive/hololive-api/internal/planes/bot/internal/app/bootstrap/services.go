package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	irisproviders "github.com/kapu/hololive-shared/pkg/providers/iris"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func InitBotInfrastructure(ctx context.Context, appConfig *apiconfig.BotPlaneConfig, infra *sharedmodules.InfraModule, deps PlaneDependencies, logger *slog.Logger) (_ *BotInfrastructure, retErr error) {
	if infra == nil {
		return nil, errors.New("shared infra must not be nil")
	}

	if deps.ACL == nil {
		return nil, errors.New("acl service must not be nil")
	}

	owner := &botInfrastructureOwner{}

	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, owner.Close())
		}
	}()

	irisClient, err := irisproviders.ProvideIrisClient(ctx, &appConfig.Iris, logger)
	if err != nil {
		return nil, fmt.Errorf("provide iris client: %w", err)
	}

	owner.irisClient = irisClient

	infrastructure, err := buildBotInfrastructureServices(ctx, appConfig, logger, infra, deps, irisClient, owner)
	if err != nil {
		return nil, fmt.Errorf("build bot infrastructure services: %w", err)
	}

	return infrastructure, nil
}

func buildBotInfrastructureServices(
	ctx context.Context,
	appConfig *apiconfig.BotPlaneConfig,
	logger *slog.Logger,
	infra *sharedmodules.InfraModule,
	planeDeps PlaneDependencies,
	irisClient irisproviders.ManagedIrisClient,
	owner *botInfrastructureOwner,
) (*BotInfrastructure, error) {
	templateRenderer := template.NewRenderer(infra.Postgres.GetPool(), logger)

	messageStrings, err := loadBotMessageStrings(ctx, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("load bot message strings: %w", err)
	}

	messageAdapter := messaging.NewMessageAdapter(appConfig.Bot.Prefix)
	formatter := messageformatter.NewResponseFormatter(appConfig.Bot.Prefix, templateRenderer, messageformatter.WithMessageStrings(messageStrings), messageformatter.WithSeeMoreFold(appConfig.Bot.SeeMoreFold), messageformatter.WithLogger(logger))

	if planeDeps.Holodex == nil {
		return nil, errors.New("holodex service must not be nil")
	}

	alarmClient, err := newAlarmClient(appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("init alarm client: %w", err)
	}

	owner.internalClients = append(owner.internalClients, alarmClient)

	integrationServices := &CoreIntegrationServices{
		ACLService:           planeDeps.ACL,
		MajorEventRepository: planeDeps.MajorEvent,
		MemberNewsService:    planeDeps.MemberNews,
	}

	deps := BuildBotDependencies(
		appConfig, infra, planeDeps.Holodex, alarmClient, integrationServices, messageAdapter, formatter, messageStrings, irisClient, logger,
	)

	return &BotInfrastructure{
		Deps:           deps,
		IrisRoomLister: irisClient,
		Postgres:       infra.Postgres,
		Cache:          infra.Cache,
		Cleanup:        owner.Close,
	}, nil
}

// 검증 실패는 기동 오류이며 운영 중 재적재는 지원하지 않는다.
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
