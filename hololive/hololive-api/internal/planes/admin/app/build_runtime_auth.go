package app

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"log/slog"

	authsvc "github.com/kapu/hololive-api/internal/planes/admin/internal/service/auth"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

func buildAdminAPIACLService(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*acl.Service, error) {
	defaultMode, err := acl.ParseACLModeStrict(appConfig.Kakao.ACLMode)
	if err != nil {
		return nil, fmt.Errorf("invalid KAKAO_ACL_MODE: %w", err)
	}

	return acl.NewACLService(
		ctx,
		infra.Postgres,
		infra.Cache,
		logger,
		appConfig.Kakao.ACLEnabled,
		defaultMode,
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
