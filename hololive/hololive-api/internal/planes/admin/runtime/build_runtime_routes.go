package adminruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	apphttp "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/dispatchops"
	"github.com/kapu/hololive-api/internal/service/activity"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	"github.com/kapu/hololive-shared/pkg/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

func buildAdminHandlers(
	infra *sharedmodules.InfraModule,
	deps Dependencies,
	alarmClient alarmProvider,
	adminSettings adminAPISettings,
	xSpaceSessions *xspaces.Store,
	schedulePosters *scheduleposter.Store,
	logger *slog.Logger,
) (*server.DomainHandlers, error) {
	var dispatchOps server.DispatchOperations

	if infra.Postgres != nil {
		dispatchOps = dispatchops.NewRepository(infra.Postgres.GetPool())
	}

	domains, err := server.NewDomainHandlers(server.Deps{
		Logger:          logger,
		Activity:        activity.NewActivityLogger(logger),
		Repository:      infra.MemberRepository,
		MemberCache:     infra.MemberCache,
		Holodex:         deps.Holodex,
		Alarm:           alarmClient,
		RoomNames:       alarmClient,
		ACL:             deps.ACL,
		Settings:        adminSettings.service,
		Applier:         adminSettings.applier,
		Iris:            deps.IrisRooms,
		DispatchOps:     dispatchOps,
		XSpaceSessions:  xSpaceSessions,
		SchedulePosters: schedulePosters,
	})
	if err != nil {
		return nil, fmt.Errorf("build admin domain handlers: %w", err)
	}

	return domains, nil
}

func buildAdminAPIRouter(
	ctx context.Context,
	appConfig *apiconfig.AdminPlaneConfig,
	infra *sharedmodules.InfraModule,
	domains *server.DomainHandlers,
	logger *slog.Logger,
) (*gin.Engine, error) {
	readyProbe := sharedreadiness.NewProbe(
		"admin",
		sharedreadiness.PostgresCheck(infra.Postgres),
		sharedreadiness.ValkeyCheck(infra.Cache),
	)

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
