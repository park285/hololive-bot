package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

	"github.com/kapu/hololive-admin/internal/server"
	"github.com/kapu/hololive-admin/internal/service/acl"
	"github.com/kapu/hololive-admin/internal/service/activity"
	authsvc "github.com/kapu/hololive-admin/internal/service/auth"
	"github.com/kapu/hololive-admin/internal/service/system"
)

// ProvideSystemCollector: 시스템 리소스 수집기를 생성하여 제공합니다.
func ProvideSystemCollector(cfg config.ServicesConfig, telemetry config.TelemetryConfig) *system.Collector {
	endpoints := []system.ServiceEndpoint{
		{Name: "llm-server", URL: cfg.LLMServerHealthURL},
		{Name: "twentyq", URL: cfg.GameBotTwentyQHealthURL},
		{Name: "turtlesoup", URL: cfg.GameBotTurtleHealthURL},
	}
	return system.NewCollector(endpoints, telemetry.Enabled)
}

// ProvideAPIHandler: Hololive API 핸들러를 생성하여 제공합니다.
func ProvideAPIHandler(
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
) *server.APIHandler {
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
	)
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
		return nil, fmt.Errorf("create auth service: %w", err)
	}
	return svc, nil
}

// ProvideAuthHandler: /api/auth 핸들러를 생성하여 제공합니다.
func ProvideAuthHandler(authService *authsvc.Service, logger *slog.Logger) *server.AuthHandler {
	return server.NewAuthHandler(authService, logger)
}

// ProvideACLService: 접근 제어 서비스 생성 (PostgreSQL 영구화)
func ProvideACLService(
	ctx context.Context,
	kakaoACLEnabled bool,
	kakaoRooms []string,
	postgres *database.PostgresService,
	cacheSvc *cache.Service,
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
		return nil, fmt.Errorf("create ACL service: %w", err)
	}
	return svc, nil
}

// ProvideActivityLogger: 활동 로거 생성
func ProvideActivityLogger(logDir string, logger *slog.Logger) *activity.Logger {
	if logDir == "" {
		return activity.NewActivityLogger("", logger)
	}
	return activity.NewActivityLogger(logDir+"/activity.log", logger)
}
