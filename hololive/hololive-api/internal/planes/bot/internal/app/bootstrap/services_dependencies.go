package bootstrap

import (
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// BuildBotDependencies는 실제 command/readiness 입력을 한 번 조립한다. 자원 수명은 plane이 소유한다.
func BuildBotDependencies(
	appConfig *apiconfig.BotPlaneConfig,
	infra *sharedmodules.InfraModule,
	holodex *holodexprovider.Service,
	alarm handlercore.AlarmService,
	integrationServices *CoreIntegrationServices,
	messageAdapter *messaging.MessageAdapter,
	formatter *messageformatter.ResponseFormatter,
	messageStrings *messagestrings.Store,
	irisClient orchestration.BotIrisClient,
	logger *slog.Logger,
) *orchestration.Dependencies {
	return &orchestration.Dependencies{
		BotSelfUser:           appConfig.Bot.SelfUser,
		IrisBaseURL:           appConfig.Iris.BaseURL,
		CalendarEntryCacheTTL: appConfig.Bot.CalendarEntryCacheTTL,
		Logger:                logger,
		Client:                irisClient,
		MessageAdapter:        messageAdapter,
		Formatter:             formatter,
		MessageStrings:        messageStrings,
		MarkdownReplies:       appConfig.Bot.MarkdownReplies,
		Cache:                 infra.Cache,
		Postgres:              infra.Postgres,
		MemberRepository:      infra.MemberRepository,
		Holodex:               holodex,
		Alarm:                 alarm,
		Matcher:               ProvideMatcher(infra.MemberCache, logger),
		MembersData:           infra.MemberCache,
		ACL:                   integrationServices.ACLService,
	}
}
