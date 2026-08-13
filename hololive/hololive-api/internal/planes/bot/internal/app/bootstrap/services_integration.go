package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-shared/pkg/service/acl"
)

func InitCoreIntegrationServices(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*CoreIntegrationServices, error) {
	defaultMode, err := acl.ParseACLModeStrict(appConfig.Kakao.ACLMode)
	if err != nil {
		return nil, fmt.Errorf("invalid KAKAO_ACL_MODE: %w", err)
	}

	aclService, err := ProvideACLService(
		ctx,
		appConfig.Kakao.ACLEnabled,
		defaultMode,
		appConfig.Kakao.Rooms,
		infra.Postgres,
		infra.Cache,
		logger,
	)
	if err != nil {
		return nil, err
	}

	majorEventRepository, memberNewsService := ResolveLLMSchedulerClients(appConfig, logger)

	return &CoreIntegrationServices{
		ACLService:           aclService,
		MajorEventRepository: majorEventRepository,
		MemberNewsService:    memberNewsService,
		CommandBuilders:      []orchcmd.CommandBuilder{},
	}, nil
}
