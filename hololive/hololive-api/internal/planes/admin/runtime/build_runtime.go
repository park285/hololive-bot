package adminruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
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

// IrisRoomLister는 bot plane이 소유한 Iris client의 방 목록 조회다. 같은 프로세스에서 직접 호출하며
// client 수명은 bot plane이 소유한다(admin drain이 끝난 뒤 닫힌다).
type IrisRoomLister interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
}

// Dependencies는 process 조립(app.BuildRuntime)이 소유하고 admin plane이 빌려 쓰는 in-process 의존성이다.
type Dependencies struct {
	// ACL은 process 하나에 하나뿐인 ACL Service다. 관리 변경은 같은 인스턴스를 쓰는 bot 판정에 바로 보인다.
	ACL *acl.Service
	// Holodex는 process 하나의 Holodex·공식 일정 서비스다(bot plane과 공유). 종료는 process 조립이 소유한다.
	Holodex *apifoundation.ScraperHolodexFoundation
	// IrisRooms가 nil이면 joined-rooms endpoint는 503을 돌려준다.
	IrisRooms IrisRoomLister
}

func BuildAdminAPIRuntime(ctx context.Context, appConfig *apiconfig.AdminPlaneConfig, infra *sharedmodules.InfraModule, deps Dependencies, logger *slog.Logger) (_ *AdminAPIRuntime, retErr error) {
	ctx, appConfig, logger, err := normalizeAdminAPIRuntimeInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("normalize admin API runtime inputs: %w", err)
	}

	if infra == nil {
		return nil, errors.New("shared infra must not be nil")
	}

	if deps.ACL == nil {
		return nil, errors.New("acl service must not be nil")
	}

	if deps.Holodex == nil || deps.Holodex.HolodexService == nil {
		return nil, errors.New("holodex foundation must not be nil")
	}

	resources := &adminRuntimeResources{logger: logger}

	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, resources.Close())
		}
	}()

	foundation := deps.Holodex

	alarmMode, err := buildAlarmModeComponents(appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: alarm mode: %w", err)
	}

	resources.alarm = alarmMode.AlarmClient

	runtime, err := buildAdminAPIRuntimeAfterAlarmMode(ctx, appConfig, infra, deps, foundation, alarmMode, resources, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin API runtime after alarm mode: %w", err)
	}

	return runtime, nil
}

func normalizeAdminAPIRuntimeInputs(
	ctx context.Context,
	appConfig *apiconfig.AdminPlaneConfig,
	logger *slog.Logger,
) (context.Context, *apiconfig.AdminPlaneConfig, *slog.Logger, error) {
	if appConfig == nil {
		return nil, nil, nil, errors.New("config must not be nil")
	}

	ctx, logger, err := bootstrap.NormalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("normalize runtime build inputs: %w", err)
	}

	return ctx, appConfig, logger, nil
}

func buildAdminAPIRuntimeAfterAlarmMode(
	ctx context.Context,
	appConfig *apiconfig.AdminPlaneConfig,
	infra *sharedmodules.InfraModule,
	deps Dependencies,
	foundation *apifoundation.ScraperHolodexFoundation,
	alarmMode *alarmModeComponents,
	resources *adminRuntimeResources,
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {
	adminSettings, err := buildAdminAPISettings(appConfig, alarmMode, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: settings: %w", err)
	}

	handler := buildAdminHandler(
		infra, foundation, alarmMode, deps.ACL, deps.IrisRooms,
		adminSettings.service, adminSettings.applier, logger,
	)
	xSpaceSessions, err := xspaces.LoadStore(infra.Postgres.GetPool())

	if err != nil && !errors.Is(err, xspaces.ErrDisabled) {
		return nil, fmt.Errorf("build admin X session store: %w", err)
	}

	handler.SetXSpaceSessions(xSpaceSessions)

	runtime, err := buildAdminAPIHTTPRuntime(ctx, appConfig, infra, handler, resources.Close, logger)
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
	appConfig *apiconfig.AdminPlaneConfig,
	infra *sharedmodules.InfraModule,
	handler *server.Handler,
	runtimeCleanup func() error,
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {
	router, err := buildAdminAPIRouter(ctx, appConfig, infra, handler, logger)
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
	appConfig *apiconfig.AdminPlaneConfig,
	logger *slog.Logger,
	router *gin.Engine,
	cleanup func() error,
) (*AdminAPIRuntime, error) {
	if appConfig == nil {
		return nil, errors.New("config must not be nil")
	}

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
