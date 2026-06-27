package bootstrap

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot"
	"github.com/kapu/hololive-shared/pkg/service/acl"
)

func InitCoreIntegrationServices(
	ctx context.Context,
	appConfig *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*CoreIntegrationServices, error) {
	aclService, err := ProvideACLService(
		ctx,
		appConfig.Kakao.ACLEnabled,
		acl.ParseACLMode(appConfig.Kakao.ACLMode),
		appConfig.Kakao.Rooms,
		infra.Postgres,
		infra.Cache,
		logger,
	)
	if err != nil {
		return nil, err
	}

	majorEventRepository, memberNewsService := ResolveLLMSchedulerClients(appConfig, logger)

	workerPool := ProvideAlarmWorkerPool(appConfig.WorkerPool)

	return &CoreIntegrationServices{
		ACLService:           aclService,
		MajorEventRepository: majorEventRepository,
		MemberNewsService:    memberNewsService,
		CommandBuilders:      []bot.CommandBuilder{},
		WorkerPool:           workerPool,
	}, nil
}
