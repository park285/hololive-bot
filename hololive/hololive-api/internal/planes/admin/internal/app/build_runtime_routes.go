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
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
	"github.com/kapu/hololive-shared/pkg/service/template"

	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/app/http"
	triggerclient "github.com/kapu/hololive-api/internal/planes/admin/internal/client/trigger"
	"github.com/kapu/hololive-api/internal/planes/admin/internal/server"
	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/system"
	"github.com/kapu/hololive-api/internal/readiness"
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
			Holodex:     foundation.HolodexService,
			YouTube:     ytStack.GetService(),
			ValkeyCache: infra.Cache,
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
	readyProbe := readiness.NewProbe("admin",
		readiness.PostgresCheck(infra.Postgres),
		readiness.ValkeyCheck(infra.Cache),
	)
	return apphttp.ProvideAPIRouter(
		ctx,
		appConfig,
		logger,
		handler.DomainHandlers(),
		server.NewAuthHandler(authService, logger),
		nil,
		nil,
		infra.Cache,
		readyProbe,
	)
}
