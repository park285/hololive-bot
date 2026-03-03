package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
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
	deps             *bot.Dependencies
	alarmService     *notification.AlarmService
	alarmCRUD        domain.AlarmCRUD
	holodexService   *holodex.Service // 구체 타입 참조 (concrete 필요 시 사용)
	ytStack          *providers.YouTubeStack
	photoSync        *holodex.PhotoSyncService
	templateRenderer *template.Renderer
	templateAdminSvc *template.AdminService
	sharedRL         *scraper.RateLimiter // YouTube 전역 RateLimiter
	cleanupCache     func()
	cleanupDB        func()
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
	// YouTube 전역 RateLimiter 생성 (1요청/3초 = 20요청/분)
	sharedRL := scraper.NewRateLimiter(3 * time.Second)
	if constants.YouTubeScraperDistributedRateLimitConfig.Enabled {
		distributedLimiter, distErr := ratelimit.NewSlidingWindowLimiter(
			infra.cacheService,
			constants.YouTubeScraperDistributedRateLimitConfig.KeyPrefix,
			logger,
		)
		if distErr != nil {
			return nil, fmt.Errorf("initialize scraper distributed rate limiter: %w", distErr)
		}
		if distErr := sharedRL.ConfigureDistributed(
			distributedLimiter,
			constants.YouTubeScraperDistributedRateLimitConfig.Limit,
			constants.YouTubeScraperDistributedRateLimitConfig.Window,
		); distErr != nil {
			return nil, fmt.Errorf("configure scraper distributed rate limiter: %w", distErr)
		}
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
	activityLogger := ProvideActivityLogger(cfg.Logging.Dir, logger)
	settingsService := providers.ProvideSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)

	aclService, err := ProvideACLService(ctx, cfg.Kakao.ACLEnabled, cfg.Kakao.Rooms, infra.postgresService, infra.cacheService, logger)
	if err != nil {
		return nil, err
	}

	var (
		majorEventRepo    command.MajorEventRepository
		memberNewsService command.MemberNewsService
	)
	if cfg.LLMSchedulerURL != "" {
		majorEventRepo = majoreventclient.New(cfg.LLMSchedulerURL, cfg.Server.APIKey)
		memberNewsService = membernewsclient.New(cfg.LLMSchedulerURL, cfg.Server.APIKey)
	} else {
		logger.Warn("LLM scheduler URL not configured; majorevent/membernews commands disabled",
			slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"),
		)
	}

	workerPool, err := ProvideAlarmWorkerPool()
	if err != nil {
		return nil, fmt.Errorf("provide alarm worker pool: %w", err)
	}

	deps := ProvideBotDependencies(
		cfg.Bot.SelfUser,
		cfg.Iris.BaseURL,
		cfg.Notification,
		logger,
		irisClient,
		messageAdapter,
		formatter,
		infra.cacheService,
		infra.postgresService,
		infra.memberRepo,
		infra.memberCache,
		holodexService,
		alarmMode.chzzkClient,
		alarmMode.twitchClient,
		profileService,
		alarmMode.alarmCRUD,
		memberMatcher,
		alarmMode.memberDataSource,
		youTubeStack,
		activityLogger,
		settingsService,
		aclService,
		majorEventRepo,
		memberNewsService,
		workerPool,
	)

	return &coreInfrastructure{
		deps:             deps,
		alarmService:     alarmMode.alarmService,
		alarmCRUD:        alarmMode.alarmCRUD,
		holodexService:   holodexService,
		ytStack:          youTubeStack,
		photoSync:        holodex.NewPhotoSyncService(holodexService, infra.memberRepo, logger),
		templateRenderer: templateRenderer,
		templateAdminSvc: template.NewAdminService(repository.NewTemplateRepository(infra.postgresService.GetGormDB(), logger), templateRenderer, logger),
		sharedRL:         sharedRL,
		cleanupCache:     infra.cleanupCache,
		cleanupDB:        infra.cleanupDB,
	}, nil
}

// ProvideACLService - 접근 제어 서비스 생성 (PostgreSQL 영구화)
func ProvideACLService(
	ctx context.Context,
	kakaoACLEnabled bool,
	kakaoRooms []string,
	postgres database.Client,
	cacheSvc cache.Client,
	logger *slog.Logger,
) (*acl.Service, error) {
	svc, err := acl.NewACLService(
		ctx,
		postgres,
		cacheSvc,
		logger,
		kakaoACLEnabled,
		kakaoRooms,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACL service: %w", err)
	}
	return svc, nil
}

// ProvideActivityLogger - 활동 로거 생성
func ProvideActivityLogger(logDir string, logger *slog.Logger) *activity.Logger {
	if logDir == "" {
		return activity.NewActivityLogger("", logger)
	}
	return activity.NewActivityLogger(logDir+"/activity.log", logger)
}

// ProvideBotDependencies - 모든 의존성을 bot.Dependencies로 조립
func ProvideBotDependencies(
	botSelfUser string,
	irisBaseURL string,
	notifCfg config.NotificationConfig,
	logger *slog.Logger,
	irisClient iris.Client,
	messageAdapter *adapter.MessageAdapter,
	formatter *adapter.ResponseFormatter,
	cacheSvc cache.Client,
	postgres database.Client,
	memberRepo *member.Repository,
	memberCache *member.Cache,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	profiles *member.ProfileService,
	alarmSvc domain.AlarmCRUD,
	memberMatcher *matcher.MemberMatcher,
	membersData member.DataProvider,
	ytStack *providers.YouTubeStack,
	activityLogger *activity.Logger,
	settingsSvc settings.ReadWriter,
	aclSvc *acl.Service,
	majorEventRepo command.MajorEventRepository,
	memberNewsSvc command.MemberNewsService,
	workerPool *workerpool.Pool,
) *bot.Dependencies {
	return &bot.Dependencies{
		BotSelfUser:      botSelfUser,
		IrisBaseURL:      irisBaseURL,
		Notification:     notifCfg,
		Logger:           logger,
		Client:           irisClient,
		MessageAdapter:   messageAdapter,
		Formatter:        formatter,
		Cache:            cacheSvc,
		Postgres:         postgres,
		MemberRepo:       memberRepo,
		MemberCache:      memberCache,
		Holodex:          holodexSvc,
		Chzzk:            chzzkClient,
		Twitch:           twitchClient,
		Profiles:         profiles,
		Alarm:            alarmSvc,
		Matcher:          memberMatcher,
		MembersData:      membersData,
		Service:          ytStack.Service,
		Scheduler:        ytStack.Scheduler,
		YouTubeStatsRepo: ytStack.StatsRepo,
		Activity:         activityLogger,
		Settings:         settingsSvc,
		ACL:              aclSvc,
		MajorEventRepo:   majorEventRepo,
		MemberNews:       memberNewsSvc,
		WorkerPool:       workerPool,
	}
}
