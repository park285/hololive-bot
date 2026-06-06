package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

type scraperHolodexProfileFoundation struct {
	HolodexService       *holodex.Service
	MemberServiceAdapter member.DataProvider
	ProfileService       *member.ProfileService
	SharedRL             *scraper.RateLimiter
}

type alarmModeComponents struct {
	AlarmCRUD        domain.AlarmCRUD
	AlarmService     *notification.AlarmService
	ChzzkClient      *chzzk.Client
	TwitchClient     *twitch.Client
	MemberDataSource member.DataProvider
}

func BuildAdminAPIRuntime(ctx context.Context, appConfig *config.Config, logger *slog.Logger) (*AdminAPIRuntime, error) {
	ctx, err := bootstrap.NormalizeRuntimeBuildInputs(ctx, appConfig, logger)
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

	aclService, err := buildAdminAPIACLService(ctx, appConfig, infra, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "acl service", err)
	}

	ytStack := buildAdminAPIYouTubeStack(ctx, appConfig, infra, foundation, logger)
	templateAdmin := buildAdminAPITemplateAdmin(infra, logger)
	authService, err := buildAdminAPIAuthService(ctx, appConfig, infra, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "auth service", err)
	}

	settingsApplier, majorEventTriggerClient := buildAdminAPISettingsApplier(appConfig, foundation, alarmMode, ytStack, logger)
	systemCollector := buildAdminAPISystemCollector(appConfig)
	communityShortsOpsRepository := buildAdminAPICommunityShortsOpsRepository(infra)
	handler := buildAdminHandler(
		appConfig,
		infra,
		foundation,
		alarmMode,
		aclService,
		ytStack,
		communityShortsOpsRepository,
		settingsApplier,
		systemCollector,
		templateAdmin,
		majorEventTriggerClient,
		logger,
	)
	router, err := buildAdminAPIRouter(ctx, appConfig, infra, authService, handler, logger)
	if err != nil {
		return cleanupAdminAPIRuntimeBuild(infra, "provide api router", err)
	}

	registerAdminAPIInternalAlarmRoutes(router, appConfig, alarmMode, logger)

	runtime, err := newAdminAPIRuntime(appConfig, logger, router, infra.Cleanup)
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
	appConfig *config.Config,
	logger *slog.Logger,
	router *gin.Engine,
	cleanup func(),
) (*AdminAPIRuntime, error) {
	servers, err := sharedserver.NewRuntimeHTTPServers(appConfig.Server, router, "hololive-admin-api.http")
	if err != nil {
		return nil, fmt.Errorf("build admin api http servers: %w", err)
	}
	return &AdminAPIRuntime{
		Config:      appConfig,
		Logger:      logger,
		ServerAddr:  servers.Addr(),
		HTTPServer:  servers.H2C,
		HTTPServers: servers,
		Managed:     lifecycle.NewManaged(cleanup),
	}, nil
}
