package app

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
	"github.com/kapu/hololive-shared/pkg/service/template"

	apphttp "github.com/kapu/hololive-admin-api/internal/app/http"
	triggerclient "github.com/kapu/hololive-admin-api/internal/client/trigger"
	"github.com/kapu/hololive-admin-api/internal/server"
	"github.com/kapu/hololive-admin-api/internal/service/system"
)

func buildAdminHandler(
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	foundation *scraperHolodexProfileFoundation,
	alarmMode *alarmModeComponents,
	aclService *acl.Service,
	ytStack *providers.YouTubeStack,
	communityShortsOpsRepository server.YouTubeCommunityShortsOpsRepository,
	settingsApplier sharedsettings.SettingsApplier,
	systemCollector *system.Collector,
	templateAdmin *template.AdminService,
	majorEventTriggerClient *triggerclient.Client,
	logger *slog.Logger,
) *server.Handler {
	return server.NewHandler(
		infra.MemberRepository,
		infra.MemberCache,
		infra.Cache,
		foundation.ProfileService,
		alarmMode.AlarmCRUD,
		foundation.HolodexService,
		ytStack.GetService(),
		ytStack.GetStatsRepository(),
		communityShortsOpsRepository,
		activity.NewActivityLogger("", logger),
		sharedmodules.BuildSettingsService(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger),
		settingsApplier,
		aclService,
		systemCollector,
		templateAdmin,
		majorEventTriggerClient,
		majorEventTriggerClient,
		logger,
	)
}

func buildAdminAPITemplateAdmin(infra *sharedmodules.InfraModule, logger *slog.Logger) *template.AdminService {
	templateRenderer := template.NewRenderer(infra.Postgres.GetPool(), logger)
	return template.NewAdminService(
		repository.NewTemplateRepository(infra.Postgres.GetPool(), logger),
		templateRenderer,
		logger,
	)
}

func buildAdminAPIRouter(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	authService *authsvc.Service,
	handler *server.Handler,
	logger *slog.Logger,
) (*gin.Engine, error) {
	return apphttp.ProvideAPIRouter(
		ctx,
		appConfig,
		logger,
		handler.DomainHandlers(),
		server.NewAuthHandler(authService, logger),
		nil,
		nil,
		infra.Cache,
	)
}

func registerAdminAPIInternalAlarmRoutes(
	router *gin.Engine,
	appConfig *config.Config,
	alarmMode *alarmModeComponents,
	logger *slog.Logger,
) {
	if alarmMode.AlarmCRUD == nil {
		return
	}

	alarmAPI := sharedalarm.NewHandler(alarmMode.AlarmCRUD, logger)
	internalAlarm := router.Group("")
	internalAlarm.Use(middleware.APIKeyAuthMiddleware(appConfig.Server.APIKey))
	alarmAPI.RegisterInternalRoutes(internalAlarm)
}
