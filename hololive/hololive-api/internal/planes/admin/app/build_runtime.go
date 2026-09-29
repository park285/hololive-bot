package app

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/kapu/hololive-shared/pkg/service/internalhttp"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

type scraperHolodexFoundation struct {
	HolodexService       *holodexprovider.Service
	MemberServiceAdapter domain.MemberDataProvider
}

type alarmModeComponents struct {
	AlarmCRUD        domain.AlarmCRUD
	MemberDataSource domain.MemberDataProvider
	// AlarmClient는 AlarmCRUD의 alarm-worker H3 transport 소유자다. plane Close에서 닫는다.
	AlarmClient io.Closer
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
		infra, foundation, alarmMode, aclService, irisRoomClient,
		communityShortsOpsRepository, adminSettings.service, adminSettings.applier, systemCollector,
		templateAdmin, adminSettings.triggerClient, logger,
	)
	xSpaceSessions, err := xspaces.LoadStore(infra.Postgres.GetPool())

	if err != nil && !errors.Is(err, xspaces.ErrDisabled) {
		infra.Cleanup()

		return nil, fmt.Errorf("build admin X session store: %w", err)
	}

	handler.SetXSpaceSessions(xSpaceSessions)

	runtimeCleanup := stopHolodexRetriesBeforeCleanup(foundation.HolodexService, closeInternalClientsBeforeCleanup(
		adminInternalClients(alarmMode, adminSettings.triggerClient, systemCollector, irisRoomClient), infra.Cleanup, logger,
	))

	runtime, err := buildAdminAPIHTTPRuntime(ctx, appConfig, infra, authService, handler, runtimeCleanup, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin APIHTTP runtime: %w", err)
	}

	runtime.ACL = aclService

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

// closeInternalClientsBeforeCleanup은 관리 plane이 만든 내부 H3 client(alarm-worker·llm-scheduler·bot 내부 API·health)를
// 닫은 뒤 infra를 닫는다. 닫힌 transport는 peer에 CONNECTION_CLOSE를 보내 peer의 graceful shutdown이 이 연결을 QUIC
// idle timeout까지 기다리지 않게 한다. 각 plane의 Close는 aggregate runtime이 모든 plane의 Shutdown(요청 drain)을
// 끝낸 뒤 불리므로 진행 중인 요청을 끊지 않는다(fxapp lifecycleCoordinator.OnStop).
func closeInternalClientsBeforeCleanup(clients []io.Closer, cleanup func(), logger *slog.Logger) func() {
	return func() {
		if err := internalhttp.CloseAll(clients...); err != nil && logger != nil {
			logger.Warn("admin_internal_client_close_failed", slog.Any("error", err))
		}

		cleanup()
	}
}

// adminInternalClients는 관리 plane이 만든 내부 H3 client를 모은다. 설정하지 않은 선택 client는 nil receiver로
// 들어오며 Close가 아무것도 하지 않는다. 내부 bot URL이 없으면 room lister 자체가 없다.
func adminInternalClients(alarmMode *alarmModeComponents, trigger, collector io.Closer, roomLister server.IrisRoomLister) []io.Closer {
	clients := []io.Closer{alarmMode.AlarmClient, trigger, collector}

	if closer, ok := roomLister.(io.Closer); ok {
		clients = append(clients, closer)
	}

	return clients
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
