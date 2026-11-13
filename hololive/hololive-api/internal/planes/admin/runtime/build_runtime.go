package adminruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/admin/internal/photosync"
	posterservice "github.com/kapu/hololive-api/internal/planes/admin/internal/scheduleposter"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

type alarmProvider interface {
	server.AlarmManager
	server.RoomNameSetter
	adminsettings.AlarmAdvanceService
	io.Closer
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
	Holodex *holodexprovider.Service
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

	if deps.Holodex == nil {
		return nil, errors.New("holodex service must not be nil")
	}

	resources := &adminRuntimeResources{logger: logger}

	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, resources.Close())
		}
	}()

	alarmClient, err := buildAlarmProvider(appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: alarm provider: %w", err)
	}

	resources.alarm = alarmClient

	adminSettings, err := buildAdminAPISettings(appConfig, alarmClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: settings: %w", err)
	}

	xSpaceSessions, err := xspaces.LoadStore(infra.Postgres.GetPool(), appConfig.XSpacesKeyFile)

	if err != nil && !errors.Is(err, xspaces.ErrDisabled) {
		return nil, fmt.Errorf("build admin X session store: %w", err)
	}

	schedulePosters, schedulePosterService, err := buildSchedulePosters(ctx, appConfig, infra, logger)
	if err != nil {
		return nil, err
	}

	domains, err := buildAdminHandlers(infra, deps, alarmClient, adminSettings, xSpaceSessions, schedulePosters,
		schedulePosterCollector(schedulePosterService), logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: %w", err)
	}

	runtime, err := buildAdminAPIHTTPRuntime(ctx, appConfig, infra, domains, resources.Close, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin APIHTTP runtime: %w", err)
	}

	attachAdminBackgroundTasks(runtime, appConfig.Ingestion.PhotoSyncEnabled, deps.Holodex, infra, schedulePosterService, logger)

	return runtime, nil
}

// schedulePosterCollector는 nil 서비스를 nil interface로 돌려준다. 그대로 넣은 typed nil은 handler의 nil 검사를 통과한다.
func schedulePosterCollector(service *posterservice.Service) server.SchedulePosterCollector {
	if service == nil {
		return nil
	}

	return service
}

// attachAdminBackgroundTasks는 nil 서비스를 interface 필드에 넣지 않는다. 넣으면 backgroundTasks의 nil 검사를 통과한다.
func attachAdminBackgroundTasks(
	runtime *AdminAPIRuntime,
	photoSyncEnabled bool,
	holodex *holodexprovider.Service,
	infra *sharedmodules.InfraModule,
	schedulePoster *posterservice.Service,
	logger *slog.Logger,
) {
	if photoSyncEnabled {
		runtime.PhotoSync = photosync.New(holodex, infra.Postgres.GetPool(), logger)
	}

	if schedulePoster != nil {
		runtime.SchedulePoster = schedulePoster
	}
}

func buildSchedulePosters(
	ctx context.Context, appConfig *apiconfig.AdminPlaneConfig, infra *sharedmodules.InfraModule, logger *slog.Logger,
) (*scheduleposter.Store, *posterservice.Service, error) {
	store, err := scheduleposter.NewStore(infra.Postgres.GetPool())
	if err != nil {
		return nil, nil, fmt.Errorf("build admin schedule poster store: %w", err)
	}

	service, err := buildSchedulePosterService(ctx, appConfig, store, infra.MemberRepository, logger)
	if err != nil && !errors.Is(err, posterservice.ErrDisabled) {
		return nil, nil, fmt.Errorf("build admin schedule poster service: %w", err)
	}

	return store, service, nil
}

// buildSchedulePosterService는 설정 파일 경로가 비어 있으면 posterservice.ErrDisabled를 감싸 돌려준다.
// 설정이 있는데 잘못되었으면 기동을 막는다.
func buildSchedulePosterService(
	ctx context.Context,
	appConfig *apiconfig.AdminPlaneConfig,
	store *scheduleposter.Store,
	members *member.Repository,
	logger *slog.Logger,
) (*posterservice.Service, error) {
	config, err := posterservice.LoadConfig(appConfig.SchedulePosterConfigFile)
	if err != nil {
		return nil, fmt.Errorf("load schedule poster config: %w", err)
	}

	apiKey, err := posterservice.LoadAPIKey(appConfig.SchedulePosterGeminiKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load schedule poster gemini api key: %w", err)
	}

	service, err := posterservice.New(ctx, config, apiKey, store, members, logger)
	if err != nil {
		return nil, fmt.Errorf("create schedule poster service: %w", err)
	}

	return service, nil
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

func buildAdminAPIHTTPRuntime(
	ctx context.Context,
	appConfig *apiconfig.AdminPlaneConfig,
	infra *sharedmodules.InfraModule,
	domains *server.DomainHandlers,
	runtimeCleanup func() error,
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {
	router, err := buildAdminAPIRouter(ctx, appConfig, infra, domains, logger)
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

	servers, err := sharedserver.NewRuntimeHTTPServers(ctx, &appConfig.Server, router, nil)
	if err != nil {
		return nil, fmt.Errorf("build admin api http servers: %w", err)
	}

	return &AdminAPIRuntime{
		Config:      appConfig,
		Logger:      logger,
		ServerAddr:  servers.Addr(),
		HTTPServers: servers,
		cleanup:     cleanup,
	}, nil
}
