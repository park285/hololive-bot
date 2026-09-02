package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	apphttp "github.com/kapu/hololive-api/internal/planes/admin/app/http"
	botroomsclient "github.com/kapu/hololive-api/internal/planes/admin/internal/client/botrooms"
	triggerclient "github.com/kapu/hololive-api/internal/planes/admin/internal/client/trigger"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	authsvc "github.com/kapu/hololive-api/internal/planes/admin/internal/service/auth"
	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/system"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-api/internal/service/activity"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func buildAdminHandler(
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	foundation *scraperHolodexProfileFoundation,
	alarmMode *alarmModeComponents,
	aclService *acl.Service,
	irisRoomClient server.IrisRoomLister,
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
			Iris:        irisRoomClient,
			SystemStats: systemCollector,
		},
		Settings: server.SettingsDeps{
			Settings: sharedmodules.BuildSettingsService(appConfig.SettingsFilePath, appConfig.Notification.AdvanceMinutes, appConfig.Scraper.ProxyEnabled, logger),
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

func buildAdminAPIBotRoomLister(appConfig *settings.Config, logger *slog.Logger) server.IrisRoomLister {
	if logger == nil {
		logger = slog.Default()
	}

	if appConfig == nil {
		logger.Warn("admin api bot room client unavailable; config is nil")

		return nil
	}

	botURL := strings.TrimSpace(appConfig.BotInternalURL)
	if botURL == "" {
		logger.Warn("admin api bot room client unavailable; joined-rooms endpoint disabled")

		return nil
	}

	client, err := botroomsclient.NewClient(botURL, appConfig.Server.APIKey, logger)
	if err != nil {
		logger.Warn("admin api bot room client unavailable; invalid bot internal url", slog.Any("error", err))

		return nil
	}

	return client
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
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	authService *authsvc.Service,
	handler *server.Handler,
	logger *slog.Logger,
) (*gin.Engine, error) {
	readyProbe := sharedreadiness.NewProbe("admin",
		sharedreadiness.PostgresCheck(infra.Postgres),
		sharedreadiness.ValkeyCheck(infra.Cache),
	)

	router, err := apphttp.ProvideAPIRouter(
		ctx,
		appConfig,
		logger,
		handler.DomainHandlers(),
		server.NewAuthHandler(authService, logger),
		infra.Cache,
		readyProbe,
	)
	if err != nil {
		return nil, fmt.Errorf("provide API router: %w", err)
	}

	return router, nil
}
