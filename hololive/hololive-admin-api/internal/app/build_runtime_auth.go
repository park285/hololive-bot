package app

import (
	"context"

	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
)

func buildAdminAPIACLService(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*acl.Service, error) {
	return acl.NewACLService(
		ctx,
		infra.Postgres,
		infra.Cache,
		logger,
		appConfig.Kakao.ACLEnabled,
		acl.ParseACLMode(appConfig.Kakao.ACLMode),
		appConfig.Kakao.Rooms,
	)
}

func buildAdminAPIAuthService(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*authsvc.Service, error) {
	authConfig := authsvc.DefaultConfig()
	authConfig.AutoPrepareSchema = appConfig.Postgres.AutoPrepareSchema
	return authsvc.NewService(ctx, infra.Postgres.GetGormDB(), infra.Cache, logger, authConfig)
}
