package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/gin-gonic/gin"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"

	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"

	"github.com/kapu/hololive-shared/pkg/service/notification/alarmservice"
	"github.com/kapu/hololive-shared/pkg/service/twitch"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

type scraperHolodexProfileFoundation struct {
	HolodexService       *holodexprovider.Service
	MemberServiceAdapter domain.MemberDataProvider
	ProfileService       *member.ProfileService
	SharedRL             *ratelimiter.RateLimiter
}

type alarmModeComponents struct {
	AlarmCRUD        domain.AlarmCRUD
	AlarmService     *alarmservice.AlarmService
	ChzzkClient      *chzzk.Client
	TwitchClient     *twitch.Client
	MemberDataSource domain.MemberDataProvider
}

func BuildAdminAPIRuntime(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (_ *AdminAPIRuntime, retErr error) {
	ctx, appConfig, err := normalizeAdminAPIRuntimeInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	infra, err := sharedmodules.BuildInfraModule(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: build infra module: %w", err)
	}

	foundation, err := buildScraperHolodexProfileFoundation(ctx, appConfig, infra, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "foundation", err)
	}

	alarmRepository := sharedalarm.NewRepository(infra.Postgres, logger)
	alarmMode, err := buildAlarmModeComponents(ctx, appConfig, infra.Cache, foundation.HolodexService, foundation.MemberServiceAdapter, alarmRepository, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "alarm mode", err)
	}
	runtimeOwnsAlarmService := false
	defer func() {
		retErr = closeUnownedAdminAlarmService(ctx, alarmMode.AlarmService, runtimeOwnsAlarmService, retErr)
	}()

	runtime, err := buildAdminAPIRuntimeAfterAlarmMode(ctx, appConfig, infra, foundation, alarmMode, logger)
	if err != nil {
		return nil, err
	}
	runtime.AlarmService = alarmMode.AlarmService
	runtimeOwnsAlarmService = true
	return runtime, nil
}

func closeUnownedAdminAlarmService(
	ctx context.Context,
	alarmService *alarmservice.AlarmService,
	runtimeOwnsAlarmService bool,
	buildErr error,
) error {
	if runtimeOwnsAlarmService || alarmService == nil {
		return buildErr
	}
	if err := alarmService.Close(ctx); err != nil {
		return errors.Join(buildErr, fmt.Errorf("build admin api runtime: close alarm service: %w", err))
	}
	return buildErr
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
		return nil, nil, err
	}
	return ctx, appConfig, nil
}

func buildAdminAPIRuntimeAfterAlarmMode(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	foundation *scraperHolodexProfileFoundation,
	alarmMode *alarmModeComponents,
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {

	aclService, err := buildAdminAPIACLService(ctx, appConfig, infra, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "acl service", err)
	}

	ytStack := buildAdminAPIYouTubeStack(ctx, appConfig, infra, foundation, logger)
	templateAdmin := buildAdminAPITemplateAdmin(infra, logger)
	authService, err := buildAdminAPIAuthService(ctx, infra, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "auth service", err)
	}

	settingsApplier, majorEventTriggerClient := buildAdminAPISettingsApplier(appConfig, foundation, alarmMode, ytStack, logger)
	systemCollector := buildAdminAPISystemCollector(appConfig)
	communityShortsOpsRepository := buildAdminAPICommunityShortsOpsRepository(infra)
	irisRoomClient := buildAdminAPIBotRoomLister(appConfig, logger)
	handler := buildAdminHandler(
		appConfig, infra, foundation, alarmMode, aclService, irisRoomClient, ytStack,
		communityShortsOpsRepository, settingsApplier, systemCollector,
		templateAdmin, majorEventTriggerClient, logger,
	)
	runtime, err := buildAdminAPIHTTPRuntime(ctx, appConfig, infra, authService, handler, logger)
	if err != nil {
		return nil, err
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
	logger *slog.Logger,
) (*AdminAPIRuntime, error) {
	router, err := buildAdminAPIRouter(ctx, appConfig, infra, authService, handler, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "provide api router", err)
	}

	runtime, err := newAdminAPIRuntime(ctx, appConfig, logger, router, infra.Cleanup)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "http server", err)
	}
	return runtime, nil
}

func cleanupAdminAPIRuntimeBuild(infra *sharedmodules.InfraModule, stage string, err error) (*AdminAPIRuntime, error) {
	infra.Cleanup()
	return nil, fmt.Errorf("build admin api runtime: %s: %w", stage, err)
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

	servers, err := sharedserver.NewRuntimeHTTPServers(ctx, &appConfig.Server, router, "hololive-admin-api.http",
		sharedserver.LocalPlaneTraceFilter)
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
