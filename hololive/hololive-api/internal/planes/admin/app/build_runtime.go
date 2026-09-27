package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	authsvc "github.com/kapu/hololive-api/internal/planes/admin/internal/service/auth"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type scraperHolodexFoundation struct {
	HolodexService       *holodexprovider.Service
	MemberServiceAdapter domain.MemberDataProvider
	SharedRL             *ratelimiter.RateLimiter
}

type alarmModeComponents struct {
	AlarmCRUD        domain.AlarmCRUD
	MemberDataSource domain.MemberDataProvider
}

func BuildAdminAPIRuntime(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (*AdminAPIRuntime, error) {
	ctx, appConfig, err := normalizeAdminAPIRuntimeInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("normalize admin API runtime inputs: %w", err)
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: build infra module: %w", err)
	}

	foundation, err := buildScraperHolodexFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin api runtime: foundation: %w", err)
	}

	alarmMode, err := buildAlarmModeComponents(appConfig, foundation.MemberServiceAdapter, logger)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin api runtime: alarm mode: %w", err)
	}

	runtime, err := buildAdminAPIRuntimeAfterAlarmMode(ctx, appConfig, infra, foundation, alarmMode, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin API runtime after alarm mode: %w", err)
	}

	return runtime, nil
}

func normalizeAdminAPIRuntimeInputs(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
) (context.Context, *settings.Config, error) {
	if appConfig == nil {
		return nil, nil, errors.New("config must not be nil")
	}

	ctx, err := bootstrap.NormalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("normalize runtime build inputs: %w", err)
	}

	return ctx, appConfig, nil
}

func buildAdminAPIRuntimeAfterAlarmMode(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	foundation *scraperHolodexFoundation,
	alarmMode *alarmModeComponents,
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {
	aclService, err := buildAdminAPIACLService(ctx, appConfig, infra, logger)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin api runtime: acl service: %w", err)
	}

	ytStack := buildAdminAPIYouTubeStack(ctx, appConfig, infra, foundation, logger)
	templateAdmin := buildAdminAPITemplateAdmin(infra, logger)

	authService, err := buildAdminAPIAuthService(appConfig, infra, logger)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin api runtime: auth service: %w", err)
	}

	adminSettings, err := buildAdminAPISettings(appConfig, alarmMode, logger)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin api runtime: settings: %w", err)
	}

	systemCollector := buildAdminAPISystemCollector(appConfig)
	communityShortsOpsRepository := buildAdminAPICommunityShortsOpsRepository(infra)

	irisRoomClient, err := buildAdminAPIBotRoomLister(appConfig, logger)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin api runtime: bot room client: %w", err)
	}

	handler := buildAdminHandler(
		infra, foundation, alarmMode, aclService, irisRoomClient, ytStack,
		communityShortsOpsRepository, adminSettings.service, adminSettings.applier, systemCollector,
		templateAdmin, adminSettings.triggerClient, logger,
	)
	xSpaceSessions, err := xspaces.LoadStore(infra.Postgres.GetPool())

	if err != nil && !errors.Is(err, xspaces.ErrDisabled) {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin X session store: %w", err)
	}

	handler.SetXSpaceSessions(xSpaceSessions)

	runtimeCleanup := stopHolodexRetriesBeforeCleanup(foundation.HolodexService, infra.Cleanup)

	runtime, err := buildAdminAPIHTTPRuntime(ctx, appConfig, infra, authService, handler, runtimeCleanup, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin APIHTTP runtime: %w", err)
	}

	if appConfig.Ingestion.PhotoSyncEnabled {
		runtime.PhotoSync = holodexprovider.NewPhotoSyncService(foundation.HolodexService, infra.MemberRepository, logger)
	}

	return runtime, nil
}

func buildAdminAPIHTTPRuntime(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	authService *authsvc.Service,
	handler *server.Handler,
	runtimeCleanup func(),
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {
	router, err := buildAdminAPIRouter(ctx, appConfig, infra, authService, handler, logger)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin api runtime: provide api router: %w", err)
	}

	runtime, err := newAdminAPIRuntime(ctx, appConfig, logger, router, runtimeCleanup)
	if err != nil {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin api runtime: http server: %w", err)
	}

	return runtime, nil
}

// stopHolodexRetriesBeforeCleanup은 Holodex 캐시 워밍 재시도를 멈춘 뒤 infra를 닫는다.
// 재시도는 예약한 요청 ctx의 취소와 분리되어 scheduler의 Stop만 끝낼 수 있다(holodexprovider retry_scheduler).
// Valkey·PG infra를 먼저 닫으면 대기 중이거나 실행 중인 재시도가 닫힌 client를 쓴다.
func stopHolodexRetriesBeforeCleanup(holodex interface{ Stop() }, cleanup func()) func() {
	return func() {
		holodex.Stop()
		cleanup()
	}
}

func newAdminAPIRuntime(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
	router *gin.Engine,
	cleanup func(),
) (*AdminAPIRuntime, error) {
	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}

	sharedserver.InitWSUpgrader(appConfig.Server.WebSocketAllowedOrigins)

	servers, err := sharedserver.NewRuntimeHTTPServers(ctx, &appConfig.Server, router, "hololive-admin-api.http",
		nil, sharedserver.LocalPlaneTraceFilter)
	if err != nil {
		return nil, fmt.Errorf("build admin api http servers: %w", err)
	}

	return &AdminAPIRuntime{
		Config:      appConfig,
		Logger:      logger,
		ServerAddr:  servers.Addr(),
		HTTPServers: servers,
		Managed:     lifecycle.NewManaged(cleanup),
	}, nil
}
