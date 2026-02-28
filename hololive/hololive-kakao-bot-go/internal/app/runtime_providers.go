package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/server"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	authsvc "github.com/kapu/hololive-kakao-bot-go/internal/service/auth"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/system"
)

// ProvideBot: 봇 인스턴스를 생성하여 제공함
func ProvideBot(deps *bot.Dependencies) (*bot.Bot, error) {
	created, err := bot.NewBot(deps)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}
	return created, nil
}

// ProvideSystemCollector: 시스템 리소스 수집기를 생성하여 제공합니다.
func ProvideSystemCollector(cfg config.ServicesConfig, telemetry config.TelemetryConfig) *system.Collector {
	endpoints := []system.ServiceEndpoint{
		{Name: "llm-server", URL: cfg.LLMServerHealthURL},
		{Name: "twentyq", URL: cfg.GameBotTwentyQHealthURL},
		{Name: "turtlesoup", URL: cfg.GameBotTurtleHealthURL},
	}
	return system.NewCollector(endpoints, telemetry.Enabled)
}

// ProvideDomainAPIHandlers: Hololive API 도메인 핸들러 묶음을 생성하여 제공한다.
func ProvideDomainAPIHandlers(
	repo *member.Repository,
	memberCache *member.Cache,
	valkeyCache *cache.Service,
	profilesSvc *member.ProfileService,
	alarm domain.AlarmCRUD,
	holodexSvc *holodex.Service,
	youtubeSvc *youtube.Service,
	scraperScheduler *poller.Scheduler,
	statsRepo *youtube.StatsRepository,
	activityLogger *activity.Logger,
	settingsSvc *settings.Service,
	settingsApplier server.SettingsApplier,
	aclSvc *acl.Service,
	systemSvc *system.Collector,
	templateAdmin *template.AdminService,
	majorEventScheduler server.MajorEventScheduler,
	majorEventMonthlyScheduler server.MajorEventMonthlyScheduler,
	logger *slog.Logger,
) *server.DomainAPIHandlers {
	return server.NewAPIHandler(
		repo,
		memberCache,
		valkeyCache,
		profilesSvc,
		alarm,
		holodexSvc,
		youtubeSvc,
		scraperScheduler,
		statsRepo,
		activityLogger,
		settingsSvc,
		settingsApplier,
		aclSvc,
		systemSvc,
		templateAdmin,
		majorEventScheduler,
		majorEventMonthlyScheduler,
		logger,
	).DomainHandlers()
}

// ProvideAuthService: 세션 기반 인증 서비스를 생성하여 제공합니다.
func ProvideAuthService(
	ctx context.Context,
	autoPrepareSchema bool,
	postgres *database.PostgresService,
	cacheSvc *cache.Service,
	logger *slog.Logger,
) (*authsvc.Service, error) {
	authCfg := authsvc.DefaultConfig()
	authCfg.AutoPrepareSchema = autoPrepareSchema
	svc, err := authsvc.NewService(ctx, postgres.GetGormDB(), cacheSvc, logger, authCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth service: %w", err)
	}
	return svc, nil
}

// ProvideAuthHandler: /api/auth 핸들러를 생성하여 제공합니다.
func ProvideAuthHandler(authService *authsvc.Service, logger *slog.Logger) *server.AuthHandler {
	return server.NewAuthHandler(authService, logger)
}

// ProvideYouTubeService: YouTube 서비스 인스턴스를 제공합니다.
func ProvideYouTubeService(ytStack *providers.YouTubeStack) *youtube.Service {
	return ytStack.Service
}

// ProvideTriggerHandler: 내부 트리거 핸들러를 생성하여 제공합니다.
func ProvideTriggerHandler(
	majorEventScheduler server.MajorEventScheduler,
	majorEventMonthlyScheduler server.MajorEventMonthlyScheduler,
	memberNewsWeeklyScheduler server.MemberNewsWeeklyScheduler,
	logger *slog.Logger,
) *server.TriggerHandler {
	return server.NewTriggerHandler(majorEventScheduler, majorEventMonthlyScheduler, memberNewsWeeklyScheduler, logger)
}

// ProvideYouTubeScheduler: YouTube 스케줄러 인스턴스를 제공합니다.
func ProvideYouTubeScheduler(deps *bot.Dependencies) *youtube.Scheduler {
	return deps.Scheduler
}
