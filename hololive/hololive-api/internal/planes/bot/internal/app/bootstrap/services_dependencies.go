package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-api/internal/apifoundation"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// BuildBotDependencies는 실제 command/readiness 입력을 한 번 조립한다. 자원 수명은 plane이 소유한다.
func BuildBotDependencies(
	appConfig *configsettings.Config,
	infra *sharedmodules.InfraModule,
	foundation *apifoundation.ScraperHolodexFoundation,
	alarmYouTubeStack *AlarmYouTubeStackComponents,
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
		Notification:          appConfig.Notification,
		CalendarImageCacheDir: appConfig.Bot.CalendarImageCacheDir,
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
		Holodex:               foundation.HolodexService,
		Alarm:                 alarmYouTubeStack.AlarmMode.AlarmCRUD,
		Matcher:               alarmYouTubeStack.Matcher,
		MembersData:           alarmYouTubeStack.AlarmMode.MemberDataSource,
		ACL:                   integrationServices.ACLService,
		MajorEventRepository:  integrationServices.MajorEventRepository,
		MemberNews:            integrationServices.MemberNewsService,
		CommandBuilders:       orchcmd.CloneCommandBuilders(integrationServices.CommandBuilders),
	}
}
