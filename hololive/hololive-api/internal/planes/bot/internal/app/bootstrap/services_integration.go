package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/service/acl"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func InitCoreIntegrationServices(
	ctx context.Context,
	appConfig *apiconfig.BotPlaneConfig,
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
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("provide ACL service: %w", err)
	}

	schedulerClients, err := ResolveLLMSchedulerClients(appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("resolve LLM scheduler clients: %w", err)
	}

	return &CoreIntegrationServices{
		ACLService:           aclService,
		MajorEventRepository: schedulerClients.MajorEvent,
		MemberNewsService:    schedulerClients.MemberNews,
		SchedulerTransports:  schedulerClients.Transports,
		CommandBuilders:      []orchcmd.CommandBuilder{},
	}, nil
}
