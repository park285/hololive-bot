package adminruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/dispatchops"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-api/internal/service/activity"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

func buildAdminHandler(
	infra *sharedmodules.InfraModule,
	foundation *apifoundation.ScraperHolodexFoundation,
	alarmMode *alarmModeComponents,
	aclService *acl.Service,
	irisRooms IrisRoomLister,
	settingsService settingssvc.ReadWriter,
	settingsApplier sharedsettings.SettingsApplier,
	logger *slog.Logger,
) *server.Handler {
	return server.NewHandler(&server.HandlerDeps{
		Common: server.CommonDeps{
			Logger:   logger,
			Activity: activity.NewActivityLogger(logger),
		},
		Member: server.MemberDeps{
			Repository: infra.MemberRepository,
			Cache:      infra.MemberCache,
		},
		Stream: server.StreamDeps{
			Holodex: foundation.HolodexService,
		},
		Stats: server.StatsDeps{
			Alarm: alarmMode.AlarmClient,
			ACL:   aclService,
			Iris:  irisRooms,
		},
		Settings: server.SettingsDeps{
			Settings:  settingsService,
			Applier:   settingsApplier,
			RoomNames: alarmMode.AlarmClient,
		},
	})
}

func buildAdminAPIRouter(
	ctx context.Context,
	appConfig *apiconfig.AdminPlaneConfig,
	infra *sharedmodules.InfraModule,
	handler *server.Handler,
	logger *slog.Logger,
) (*gin.Engine, error) {
	readyProbe := sharedreadiness.NewProbe(
		"admin",
		sharedreadiness.PostgresCheck(infra.Postgres),
		sharedreadiness.ValkeyCheck(infra.Cache),
	)

	domains := handler.DomainHandlers()

	if infra.Postgres != nil {
		domains.Alarm.SetDispatchOperations(dispatchops.NewRepository(infra.Postgres.GetPool()))
	}

	router, err := apphttp.ProvideAPIRouter(
		ctx,
		appConfig,
		logger,
		domains,
		infra.Cache,
		readyProbe,
	)
	if err != nil {
		return nil, fmt.Errorf("provide API router: %w", err)
	}

	return router, nil
}
