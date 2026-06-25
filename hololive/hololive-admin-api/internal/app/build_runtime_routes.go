package app

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/repository"
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
	return server.NewHandler(&server.HandlerDeps{
		Common: server.CommonDeps{
			Logger:   logger,
			Activity: activity.NewActivityLogger("", logger),
		},
		Member: server.MemberDeps{
			Repository: infra.MemberRepository,
			Cache:      infra.MemberCache,
			Profiles:   foundation.ProfileService,
		},
		Stream: server.StreamDeps{
			Holodex:         foundation.HolodexService,
			YouTube:         ytStack.GetService(),
			ValkeyCache:     infra.Cache,
			StatsRepository: ytStack.GetStatsRepository(),
		},
		Stats: server.StatsDeps{
			Alarm:       alarmMode.AlarmCRUD,
			ACL:         aclService,
			SystemStats: systemCollector,
		},
		Settings: server.SettingsDeps{
			Settings: sharedmodules.BuildSettingsService(appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger),
			Applier:  settingsApplier,
		},
		Template: server.TemplateDeps{
			Admin: templateAdmin,
		},
		MajorEvent: server.MajorEventDeps{
			Scheduler:        majorEventTriggerClient,
			MonthlyScheduler: majorEventTriggerClient,
		},
		YouTubeOps: server.YouTubeOpsDeps{
			CommunityShortsOps: communityShortsOpsRepository,
		},
	})
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

	registrar := sharedalarm.NewInternalRouteRegistrar(appConfig.Server.APIKey, alarmMode.AlarmCRUD, logger)
	if err := registrar(router); err != nil && logger != nil {
		logger.Error("admin-api alarm compatibility route registration failed", slog.Any("error", err))
	}
}
