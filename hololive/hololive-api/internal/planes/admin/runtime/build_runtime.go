package adminruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-api/internal/apifoundation"
	adminhandlers "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi/handlers"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	authsvc "github.com/kapu/hololive-api/internal/planes/admin/internal/service/auth"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

// alarmProvider는 관리 plane의 실제 consumer ports와 transport 소유권만 결합한다.
type alarmProvider interface {
	server.AlarmManager
	server.RoomNameSetter
	sharedsettings.AlarmAdvanceService
	io.Closer
}

type alarmModeComponents struct {
	AlarmClient alarmProvider
}

func BuildAdminAPIRuntime(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (_ *AdminAPIRuntime, retErr error) {
	ctx, appConfig, err := normalizeAdminAPIRuntimeInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("normalize admin API runtime inputs: %w", err)
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, sharedmodules.InfraOptions{Valkey: appConfig.Valkey, Postgres: appConfig.Postgres}, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: build infra module: %w", err)
	}

	resources := &adminRuntimeResources{infra: infra, logger: logger}

	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, resources.Close())
		}
	}()

	foundation, err := apifoundation.BuildScraperHolodex(ctx, apifoundation.ScraperHolodexOptions{
		Holodex:          appConfig.Holodex,
		OfficialSchedule: appConfig.OfficialScheduleRuntime(),
	}, infra.MemberCache, infra.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: foundation: %w", err)
	}

	resources.holodex = foundation.HolodexService

	alarmMode, err := buildAlarmModeComponents(appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: alarm mode: %w", err)
	}

	resources.alarm = alarmMode.AlarmClient

	runtime, err := buildAdminAPIRuntimeAfterAlarmMode(ctx, appConfig, infra, foundation, alarmMode, resources, logger)
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
	foundation *apifoundation.ScraperHolodexFoundation,
	alarmMode *alarmModeComponents,
	resources *adminRuntimeResources,
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {
	aclService, err := buildAdminAPIACLService(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: acl service: %w", err)
	}

	templateAdmin := buildAdminAPITemplateAdmin(infra, logger)

	authService, err := buildAdminAPIAuthService(appConfig, infra, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: auth service: %w", err)
	}

	adminSettings, err := buildAdminAPISettings(appConfig, alarmMode, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: settings: %w", err)
	}

	resources.trigger = adminSettings.triggerClient

	systemCollector := buildAdminAPISystemCollector(appConfig)

	resources.collector = systemCollector

	communityShortsOpsRepository := buildAdminAPICommunityShortsOpsRepository(infra)

	irisRoomClient, err := buildAdminAPIBotRoomLister(appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: bot room client: %w", err)
	}

	if closer, ok := irisRoomClient.(io.Closer); ok {
		resources.rooms = closer
	}

	handler := buildAdminHandler(
		infra, foundation, alarmMode, aclService, irisRoomClient,
		communityShortsOpsRepository, adminSettings.service, adminSettings.applier, systemCollector,
		templateAdmin, adminSettings.triggerClient, logger,
	)
	xSpaceSessions, err := xspaces.LoadStore(infra.Postgres.GetPool())

	if err != nil && !errors.Is(err, xspaces.ErrDisabled) {
		return nil, fmt.Errorf("build admin X session store: %w", err)
	}

	handler.SetXSpaceSessions(xSpaceSessions)

	runtime, err := buildAdminAPIHTTPRuntime(ctx, appConfig, infra, authService, handler, resources.Close, logger)
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
	runtimeCleanup func() error,
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {
	router, err := buildAdminAPIRouter(ctx, appConfig, infra, authService, handler, logger)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("build admin api runtime: provide api router: %w", err), runtimeCleanup())
	}

	runtime, err := newAdminAPIRuntime(ctx, appConfig, logger, router, runtimeCleanup)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("build admin api runtime: http server: %w", err), runtimeCleanup())
	}

	return runtime, nil
}

func newAdminAPIRuntime(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
	router *gin.Engine,
	cleanup func() error,
) (*AdminAPIRuntime, error) {
	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}

	adminhandlers.InitWSUpgrader(appConfig.Server.WebSocketAllowedOrigins)

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
		Managed: lifecycle.NewManaged(func() {
			if cleanup != nil {
				if cleanupErr := cleanup(); cleanupErr != nil && logger != nil {
					logger.Warn("admin_resource_cleanup_failed", slog.Any("error", cleanupErr))
				}
			}
		}),
		cleanup: cleanup,
	}, nil
}
