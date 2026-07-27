package bootstrap

import (
	"log/slog"

	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"

	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

func BuildBotDependencyModules(
	appConfig *configsettings.Config,
	infra *sharedmodules.InfraModule,
	foundation *ScraperHolodexProfileFoundation,
	alarmYouTubeStack *AlarmYouTubeStackComponents,
	integrationServices *CoreIntegrationServices,
	messageAdapter *messaging.MessageAdapter,
	formatter *messageformatter.ResponseFormatter,
	messageStrings *messagestrings.Store,
	irisClient iris.BotClient,
	logger *slog.Logger,
) BotDependencyModules {
	return BotDependencyModules{
		Core:      buildBotCoreModule(appConfig, logger),
		Messaging: buildBotMessagingModule(irisClient, messageAdapter, formatter, messageStrings),
		Data:      buildBotDataModule(infra, alarmYouTubeStack.AlarmMode, foundation.ProfileService),
		Stream:    buildBotStreamModule(alarmYouTubeStack.AlarmMode, foundation.HolodexService, alarmYouTubeStack.Matcher, alarmYouTubeStack.YouTubeStack),
		Support:   buildBotSupportModule(alarmYouTubeStack.ActivityLogger, alarmYouTubeStack.SettingsService, integrationServices.ACLService, integrationServices.WorkerPool),
		Feature:   buildBotFeatureModule(integrationServices.MajorEventRepository, integrationServices.MemberNewsService, integrationServices.CommandBuilders),
	}
}

func buildBotCoreModule(appConfig *configsettings.Config, logger *slog.Logger) BotCoreModule {
	return BotCoreModule{
		BotSelfUser:           appConfig.Bot.SelfUser,
		IrisBaseURL:           appConfig.Iris.BaseURL,
		Notification:          appConfig.Notification,
		CalendarImageCacheDir: appConfig.Bot.CalendarImageCacheDir,
		CalendarEntryCacheTTL: appConfig.Bot.CalendarEntryCacheTTL,
		Logger:                logger,
	}
}

func buildBotMessagingModule(
	irisClient iris.BotClient,
	messageAdapter *messaging.MessageAdapter,
	formatter *messageformatter.ResponseFormatter,
	messageStrings *messagestrings.Store,
) BotMessagingModule {
	return BotMessagingModule{
		Client:         irisClient,
		MessageAdapter: messageAdapter,
		Formatter:      formatter,
		MessageStrings: messageStrings,
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
	holodexService *holodexprovider.Service,
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
	workerPool *workerpool.QueuedPool,
) BotSupportModule {
	return BotSupportModule{
		ActivityLogger: activityLogger,
		Settings:       settingsService,
		ACL:            aclService,
		WorkerPool:     workerPool,
	}
}

func buildBotFeatureModule(
	majorEventRepository handlercore.MajorEventRepository,
	memberNewsService handlercore.MemberNewsService,
	commandBuilders []orchcmd.CommandBuilder,
) BotFeatureModule {
	return BotFeatureModule{
		MajorEventRepository: majorEventRepository,
		MemberNews:           memberNewsService,
		CommandBuilders:      orchcmd.CloneCommandBuilders(commandBuilders),
	}
}
