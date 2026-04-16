package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
)

func BuildBotDependencyModules(
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	alarmMode *AlarmModeComponents,
	holodexService *holodex.Service,
	messageAdapter *adapter.MessageAdapter,
	formatter *adapter.ResponseFormatter,
	irisClient BotIrisClient,
	profileService *member.ProfileService,
	memberMatcher *matcher.MemberMatcher,
	youTubeStack *providers.YouTubeStack,
	activityLogger *activity.Logger,
	settingsService settings.ReadWriter,
	aclService *acl.Service,
	majorEventRepo command.MajorEventRepository,
	memberNewsService command.MemberNewsService,
	commandBuilders []bot.CommandBuilder,
	workerPool *workerpool.Pool,
	logger *slog.Logger,
) BotDependencyModules {
	return BotDependencyModules{
		Core: BotCoreModule{
			BotSelfUser:  cfg.Bot.SelfUser,
			IrisBaseURL:  cfg.Iris.BaseURL,
			Notification: cfg.Notification,
			Logger:       logger,
		},
		Messaging: BotMessagingModule{
			Client:         irisClient,
			MessageAdapter: messageAdapter,
			Formatter:      formatter,
		},
		Data: BotDataModule{
			CacheSvc:    infra.Cache,
			Postgres:    infra.Postgres,
			MemberRepo:  infra.MemberRepo,
			MemberCache: infra.MemberCache,
			Profiles:    profileService,
			MembersData: alarmMode.MemberDataSource,
		},
		Stream: BotStreamModule{
			HolodexSvc:   holodexService,
			ChzzkClient:  alarmMode.ChzzkClient,
			TwitchClient: alarmMode.TwitchClient,
			AlarmSvc:     alarmMode.AlarmCRUD,
			MemberMatch:  memberMatcher,
			YTStack:      youTubeStack,
		},
		Support: BotSupportModule{
			ActivityLogger: activityLogger,
			SettingsSvc:    settingsService,
			ACLSvc:         aclService,
			WorkerPool:     workerPool,
		},
		Feature: BotFeatureModule{
			MajorEventRepo:  majorEventRepo,
			MemberNewsSvc:   memberNewsService,
			CommandBuilders: bot.CloneCommandBuilders(commandBuilders),
		},
	}
}
