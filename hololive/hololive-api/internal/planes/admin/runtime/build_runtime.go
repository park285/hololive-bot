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
	"github.com/kapu/hololive-api/internal/service/geminidecision"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

type alarmProvider interface {
	server.AlarmManager
	server.RoomNameSetter
	adminsettings.AlarmAdvanceService
	io.Closer
}

type IrisRoomLister interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
}

type Dependencies struct {
	ACL       *acl.Service
	Holodex   *holodexprovider.Service
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

func schedulePosterCollector(service *posterservice.Service) server.SchedulePosterCollector {
	if service == nil {
		return nil
	}

	return service
}

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

	apiKey, err := loadSchedulePosterAPIKey(appConfig.SchedulePosterGeminiKeyFile)
	if err != nil {
		return nil, err
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

func loadSchedulePosterAPIKey(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	key, err := geminidecision.LoadAPIKey(path)
	if err != nil {
		return "", fmt.Errorf("load schedule poster gemini api key: %w", err)
	}

	return key, nil
}
