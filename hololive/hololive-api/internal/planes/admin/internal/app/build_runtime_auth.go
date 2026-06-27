package app

import (
	"context"

	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
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
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*authsvc.Service, error) {
	authConfig := authsvc.DefaultConfig()
	// bcrypt cost는 env로 조정 가능. 범위 밖 값은 NewService가 안전 기본값으로 보정한다.
	authConfig.BcryptCost = sharedenv.Int("AUTH_BCRYPT_COST", authsvc.DefaultBcryptCost)
	return authsvc.NewService(ctx, infra.Postgres.GetPool(), infra.Cache, logger, authConfig)
}
