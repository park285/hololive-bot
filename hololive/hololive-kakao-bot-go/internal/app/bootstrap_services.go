package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/majoreventclient"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/membernewsclient"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

// coreInfrastructure 는 Bot 런타임 구성에 필요한 의존성/서비스 묶음을 담는다.
type coreInfrastructure struct {
	deps                         *bot.Dependencies
	alarmService                 *notification.AlarmService
	alarmCRUD                    domain.AlarmCRUD
	holodexService               *holodex.Service // 구체 타입 참조 (concrete 필요 시 사용)
	ytStack                      *providers.YouTubeStack
	photoSync                    *holodex.PhotoSyncService
	templateRenderer             *template.Renderer
	templateAdminSvc             *template.AdminService
	sharedRL                     *scraper.RateLimiter // YouTube 전역 RateLimiter
	runtimeAlarmSchedulerBuilder runtimeAlarmSchedulerBuilder
	cleanupCache                 func()
	cleanupDB                    func()
}

type alarmModeComponents struct {
	alarmCRUD        domain.AlarmCRUD
	alarmService     *notification.AlarmService
	chzzkClient      *chzzk.Client
	twitchClient     *twitch.Client
	memberDataSource member.DataProvider
}

type alarmDependencies struct {
	alarmService       *notification.AlarmService
	memberDataProvider member.DataProvider
	chzzkClient        *chzzk.Client
	twitchClient       *twitch.Client
}

type botCoreModule struct {
	botSelfUser  string
	irisBaseURL  string
	notification config.NotificationConfig
	logger       *slog.Logger
}

type botMessagingModule struct {
	client         iris.Client
	messageAdapter *adapter.MessageAdapter
	formatter      *adapter.ResponseFormatter
}

type botDataModule struct {
	cacheSvc    cache.Client
	postgres    database.Client
	memberRepo  *member.Repository
	memberCache *member.Cache
	profiles    *member.ProfileService
	membersData member.DataProvider
}

type botStreamModule struct {
	holodexSvc   *holodex.Service
	chzzkClient  *chzzk.Client
	twitchClient *twitch.Client
	alarmSvc     domain.AlarmCRUD
	memberMatch  *matcher.MemberMatcher
	ytStack      *providers.YouTubeStack
}

type botSupportModule struct {
	activityLogger *activity.Logger
	settingsSvc    settings.ReadWriter
	aclSvc         *acl.Service
	workerPool     *workerpool.Pool
}

type botFeatureModule struct {
	majorEventRepo   command.MajorEventRepository
	memberNewsSvc    command.MemberNewsService
	commandFactories []command.Factory
}

type botDependencyModules struct {
	core      botCoreModule
	messaging botMessagingModule
	data      botDataModule
	stream    botStreamModule
	support   botSupportModule
	feature   botFeatureModule
}

func initAlarmDependencies(
	chzzkCfg config.ChzzkConfig,
	twitchCfg config.TwitchConfig,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService cache.Client,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmDependencies, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	chzzkClient := ProvideChzzkClient(httpClient, chzzkCfg, logger)
	twitchClient := ProvideTwitchClient(twitchCfg, logger)
	memberDataProvider := providers.ProvideMembersData(memberServiceAdapter)

	resolved := providers.ResolveAlarmAdvanceMinutes(advanceMinutes, scraperProxyEnabled, logger)
	alarmService, err := ProvideAlarmService(resolved, cacheService, holodexService, chzzkClient, twitchClient, memberDataProvider, alarmRepository, logger)
	if err != nil {
		return nil, fmt.Errorf("provide alarm service: %w", err)
	}

	return &alarmDependencies{
		alarmService:       alarmService,
		memberDataProvider: memberDataProvider,
		chzzkClient:        chzzkClient,
		twitchClient:       twitchClient,
	}, nil
}

func initAlarmModeComponents(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmModeComponents, error) {
	alarmDeps, alarmErr := initAlarmDependencies(
		cfg.Chzzk,
		cfg.Twitch,
		cfg.Notification.AdvanceMinutes,
		cfg.Scraper.ProxyEnabled,
		infra.cacheService,
		holodexService,
		memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if alarmErr != nil {
		return nil, alarmErr
	}

	if warnErr := alarmDeps.alarmService.WarmCacheFromDB(ctx); warnErr != nil {
		logger.Warn("Failed to warm alarm cache from DB", "error", warnErr)
	}

	return &alarmModeComponents{
		alarmCRUD:        alarmDeps.alarmService,
		alarmService:     alarmDeps.alarmService,
		chzzkClient:      alarmDeps.chzzkClient,
		twitchClient:     alarmDeps.twitchClient,
		memberDataSource: alarmDeps.memberDataProvider,
	}, nil
}

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

// initCoreInfrastructure 는 공통 인프라를 초기화한다.
//
//nolint:funlen // bootstrap wiring; keep the dependency graph visible in one place
func initCoreInfrastructure(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *coreInfrastructure, retErr error) {
	irisClient := providers.ProvideIrisClient(cfg.Iris, logger)

	infra, err := initInfraResources(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil {
			infra.cleanupDB()
			infra.cleanupCache()
		}
	}()

	templateRenderer := template.NewRenderer(infra.postgresService.GetGormDB(), logger)
	messageAdapter := adapter.NewMessageAdapter(cfg.Bot.Prefix)
	formatter := adapter.NewResponseFormatter(cfg.Bot.Prefix, templateRenderer)

	holodexAPIKeys := providers.ProvideHolodexAPIKeys(cfg.Holodex)
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(infra.memberCache, logger)
	scraperProxyConfig := scraper.ProxyConfig{
		Enabled: cfg.Scraper.ProxyEnabled,
		URL:     cfg.Scraper.ProxyURL,
	}
	sharedRL, err := providers.ProvideYouTubeScraperRateLimiter(infra.cacheService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide youtube scraper rate limiter: %w", err)
	}
	scraperService := providers.ProvideScraperService(infra.cacheService, memberServiceAdapter, scraperProxyConfig, sharedRL, logger)
	holodexService, err := providers.ProvideHolodexService(cfg.Holodex.BaseURL, holodexAPIKeys, infra.cacheService, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	profileService, err := providers.ProvideProfileService(ctx, infra.cacheService, memberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("provide profile service: %w", err)
	}

	alarmRepository := ProvideAlarmRepository(infra.postgresService, logger)
	alarmMode, err := initAlarmModeComponents(ctx, cfg, infra, holodexService, memberServiceAdapter, alarmRepository, logger)
	if err != nil {
		return nil, err
	}

	memberMatcher := ProvideMemberMatcher(ctx, alarmMode.memberDataSource, infra.cacheService, holodexService, logger)
	youTubeStatsRepository := providers.ProvideYouTubeStatsRepository(infra.postgresService, logger)
	youTubeStack := providers.ProvideYouTubeStack(ctx, cfg.YouTube, cfg.Scraper, infra.cacheService, holodexService, memberServiceAdapter, youTubeStatsRepository, alarmMode.alarmService, irisClient, formatter, sharedRL, logger)
	activityLogger := ProvideActivityLogger(logger)
	settingsService := providers.ProvideSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)

	aclService, err := ProvideACLService(ctx, cfg.Kakao.ACLEnabled, cfg.Kakao.Rooms, infra.postgresService, infra.cacheService, logger)
	if err != nil {
		return nil, err
	}

	majorEventRepo, memberNewsService := resolveLLMSchedulerClients(cfg, logger)

	workerPool, err := ProvideAlarmWorkerPool()
	if err != nil {
		return nil, fmt.Errorf("provide alarm worker pool: %w", err)
	}

	modules := buildBotDependencyModules(
		cfg,
		infra,
		alarmMode,
		holodexService,
		messageAdapter,
		formatter,
		irisClient,
		profileService,
		memberMatcher,
		youTubeStack,
		activityLogger,
		settingsService,
		aclService,
		majorEventRepo,
		memberNewsService,
		workerPool,
		logger,
	)
	deps := ProvideBotDependencies(modules)

	return &coreInfrastructure{
		deps:                         deps,
		alarmService:                 alarmMode.alarmService,
		alarmCRUD:                    alarmMode.alarmCRUD,
		holodexService:               holodexService,
		ytStack:                      youTubeStack,
		photoSync:                    holodex.NewPhotoSyncService(holodexService, infra.memberRepo, logger),
		templateRenderer:             templateRenderer,
		templateAdminSvc:             template.NewAdminService(repository.NewTemplateRepository(infra.postgresService.GetGormDB(), logger), templateRenderer, logger),
		sharedRL:                     sharedRL,
		runtimeAlarmSchedulerBuilder: defaultRuntimeAlarmSchedulerBuilder,
		cleanupCache:                 infra.cleanupCache,
		cleanupDB:                    infra.cleanupDB,
	}, nil
}
