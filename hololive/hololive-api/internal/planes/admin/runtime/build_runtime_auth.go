package adminruntime

import (
	"context"
	"fmt"
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/service/acl"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func buildAdminAPIACLService(
	ctx context.Context,
	appConfig *apiconfig.AdminPlaneConfig,
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
