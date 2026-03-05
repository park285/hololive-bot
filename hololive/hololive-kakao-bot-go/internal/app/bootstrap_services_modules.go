package app

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/majoreventclient"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/membernewsclient"
)

func resolveLLMSchedulerClients(
	cfg *config.Config,
	logger *slog.Logger,
) (command.MajorEventRepository, command.MemberNewsService) {
	if cfg.LLMSchedulerURL == "" {
		logger.Warn("LLM scheduler URL not configured; majorevent/membernews commands disabled",
			slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"),
		)
		return nil, nil
	}

	return majoreventclient.New(cfg.LLMSchedulerURL, cfg.Server.APIKey),
		membernewsclient.New(cfg.LLMSchedulerURL, cfg.Server.APIKey)
}

func buildBotDependencyModules(
	cfg *config.Config,
	infra *infraResources,
	alarmMode *alarmModeComponents,
	holodexService *holodex.Service,
	messageAdapter *adapter.MessageAdapter,
	formatter *adapter.ResponseFormatter,
	irisClient iris.Client,
	profileService *member.ProfileService,
	memberMatcher *matcher.MemberMatcher,
	youTubeStack *providers.YouTubeStack,
	activityLogger *activity.Logger,
	settingsService settings.ReadWriter,
	aclService *acl.Service,
	majorEventRepo command.MajorEventRepository,
	memberNewsService command.MemberNewsService,
	workerPool *workerpool.Pool,
	logger *slog.Logger,
) botDependencyModules {
	return botDependencyModules{
		core: botCoreModule{
			botSelfUser:  cfg.Bot.SelfUser,
			irisBaseURL:  cfg.Iris.BaseURL,
			notification: cfg.Notification,
			logger:       logger,
		},
		messaging: botMessagingModule{
			client:         irisClient,
			messageAdapter: messageAdapter,
			formatter:      formatter,
		},
		data: botDataModule{
			cacheSvc:    infra.cacheService,
			postgres:    infra.postgresService,
			memberRepo:  infra.memberRepo,
			memberCache: infra.memberCache,
			profiles:    profileService,
			membersData: alarmMode.memberDataSource,
		},
		stream: botStreamModule{
			holodexSvc:   holodexService,
			chzzkClient:  alarmMode.chzzkClient,
			twitchClient: alarmMode.twitchClient,
			alarmSvc:     alarmMode.alarmCRUD,
			memberMatch:  memberMatcher,
			ytStack:      youTubeStack,
		},
		support: botSupportModule{
			activityLogger: activityLogger,
			settingsSvc:    settingsService,
			aclSvc:         aclService,
			workerPool:     workerPool,
		},
		feature: botFeatureModule{
			majorEventRepo: majorEventRepo,
			memberNewsSvc:  memberNewsService,
		},
	}
}
