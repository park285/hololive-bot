package app

import (
	"context"
	"fmt"
	"log/slog"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	authsvc "github.com/kapu/hololive-api/internal/planes/admin/internal/service/auth"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
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

	aclService, err := acl.NewACLService(
		ctx,
		infra.Postgres,
		infra.Cache,
		logger,
		appConfig.Kakao.ACLEnabled,
		defaultMode,
		appConfig.Kakao.Rooms,
	)
	if err != nil {
		return nil, fmt.Errorf("ACL service: %w", err)
	}

	return aclService, nil
}

func buildAdminAPIAuthService(
	ctx context.Context,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*authsvc.Service, error) {
	authConfig := authsvc.DefaultConfig()
	// bcrypt cost는 env로 조정 가능. 범위 밖 값은 NewService가 안전 기본값으로 보정한다.
	authConfig.BcryptCost = sharedenv.Int("AUTH_BCRYPT_COST", authsvc.DefaultBcryptCost)

	authService, err := authsvc.NewService(ctx, infra.Postgres.GetPool(), infra.Cache, logger, authConfig)
	if err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	return authService, nil
}
