package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/template"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-shared/pkg/service/acl"
)

func InitCoreIntegrationServices(
	ctx context.Context,
	cfg *config.Config,
	infra *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*CoreIntegrationServices, error) {
	aclService, err := ProvideACLService(
		ctx,
		cfg.Kakao.ACLEnabled,
		acl.ParseACLMode(cfg.Kakao.ACLMode),
		cfg.Kakao.Rooms,
		infra.Postgres,
		infra.Cache,
		logger,
	)
	if err != nil {
		return nil, err
	}

	majorEventRepo, memberNewsService := ResolveLLMSchedulerClients(cfg, logger)

	workerPool, err := ProvideAlarmWorkerPool()
	if err != nil {
		return nil, fmt.Errorf("provide alarm worker pool: %w", err)
	}

	return &CoreIntegrationServices{
		ACLService:        aclService,
		MajorEventRepo:    majorEventRepo,
		MemberNewsService: memberNewsService,
		CommandBuilders:   []bot.CommandBuilder{},
		WorkerPool:        workerPool,
	}, nil
}

func BuildTemplateAdminService(
	infra *sharedmodules.InfraModule,
	templateRenderer *template.Renderer,
	logger *slog.Logger,
) *template.AdminService {
	return template.NewAdminService(
		repository.NewTemplateRepository(infra.Postgres.GetGormDB(), logger),
		templateRenderer,
		logger,
	)
}
