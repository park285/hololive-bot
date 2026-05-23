package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/hololive-bot/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
)

func BuildBotDependencyModules(
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	alarmMode *AlarmModeComponents,
	holodexService *holodex.Service,
	messageAdapter *adapter.MessageAdapter,
	formatter *adapter.ResponseFormatter,
	irisClient BotIrisClient,
	profileService *member.ProfileService,
	memberMatcher *matcher.Matcher,
	youTubeStack *providers.YouTubeStack,
	activityLogger *activity.Logger,
	settingsService settings.ReadWriter,
	aclService *acl.Service,
	majorEventRepository command.MajorEventRepository,
	memberNewsService command.MemberNewsService,
	commandBuilders []bot.CommandBuilder,
	workerPool *workerpool.Pool,
	logger *slog.Logger,
) BotDependencyModules {
	return BotDependencyModules{
		Core:      buildBotCoreModule(appConfig, logger),
		Messaging: buildBotMessagingModule(irisClient, messageAdapter, formatter),
		Data:      buildBotDataModule(infra, alarmMode, profileService),
		Stream:    buildBotStreamModule(alarmMode, holodexService, memberMatcher, youTubeStack),
		Support:   buildBotSupportModule(activityLogger, settingsService, aclService, workerPool),
		Feature:   buildBotFeatureModule(majorEventRepository, memberNewsService, commandBuilders),
	}
}

func buildBotCoreModule(appConfig *config.Config, logger *slog.Logger) BotCoreModule {
	return BotCoreModule{
		BotSelfUser:  appConfig.Bot.SelfUser,
		IrisBaseURL:  appConfig.Iris.BaseURL,
		Notification: appConfig.Notification,
		Logger:       logger,
	}
}

func buildBotMessagingModule(
	irisClient BotIrisClient,
	messageAdapter *adapter.MessageAdapter,
	formatter *adapter.ResponseFormatter,
) BotMessagingModule {
	return BotMessagingModule{
		Client:         irisClient,
		MessageAdapter: messageAdapter,
		Formatter:      formatter,
	}
}

func buildBotDataModule(
	infra *sharedmodules.InfraModule,
	alarmMode *AlarmModeComponents,
	profileService *member.ProfileService,
) BotDataModule {
	return BotDataModule{
		Cache:            infra.Cache,
		Postgres:         infra.Postgres,
		MemberRepository: infra.MemberRepository,
		MemberCache:      infra.MemberCache,
		Profiles:         profileService,
		MembersData:      alarmMode.MemberDataSource,
	}
}

func buildBotStreamModule(
	alarmMode *AlarmModeComponents,
	holodexService *holodex.Service,
	memberMatcher *matcher.Matcher,
	youTubeStack *providers.YouTubeStack,
) BotStreamModule {
	return BotStreamModule{
		Holodex:      holodexService,
		ChzzkClient:  alarmMode.ChzzkClient,
		TwitchClient: alarmMode.TwitchClient,
		Alarm:        alarmMode.AlarmCRUD,
		MemberMatch:  memberMatcher,
		YTStack:      youTubeStack,
	}
}

func buildBotSupportModule(
	activityLogger *activity.Logger,
	settingsService settings.ReadWriter,
	aclService *acl.Service,
	workerPool *workerpool.Pool,
) BotSupportModule {
	return BotSupportModule{
		ActivityLogger: activityLogger,
		Settings:       settingsService,
		ACL:            aclService,
		WorkerPool:     workerPool,
	}
}

func buildBotFeatureModule(
	majorEventRepository command.MajorEventRepository,
	memberNewsService command.MemberNewsService,
	commandBuilders []bot.CommandBuilder,
) BotFeatureModule {
	return BotFeatureModule{
		MajorEventRepository: majorEventRepository,
		MemberNews:           memberNewsService,
		CommandBuilders:      bot.CloneCommandBuilders(commandBuilders),
	}
}
