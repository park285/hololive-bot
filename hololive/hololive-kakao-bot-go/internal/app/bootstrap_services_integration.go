package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
)

type coreIntegrationServices struct {
	aclService        *acl.Service
	majorEventRepo    command.MajorEventRepository
	memberNewsService command.MemberNewsService
	workerPool        *workerpool.Pool
}

func initCoreIntegrationServices(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	logger *slog.Logger,
) (*coreIntegrationServices, error) {
	aclService, err := ProvideACLService(
		ctx,
		cfg.Kakao.ACLEnabled,
		cfg.Kakao.Rooms,
		infra.postgresService,
		infra.cacheService,
		logger,
	)
	if err != nil {
		return nil, err
	}

	majorEventRepo, memberNewsService := resolveLLMSchedulerClients(cfg, logger)

	workerPool, err := ProvideAlarmWorkerPool()
	if err != nil {
		return nil, fmt.Errorf("provide alarm worker pool: %w", err)
	}

	return &coreIntegrationServices{
		aclService:        aclService,
		majorEventRepo:    majorEventRepo,
		memberNewsService: memberNewsService,
		workerPool:        workerPool,
	}, nil
}

func buildTemplateAdminService(
	infra *infraResources,
	templateRenderer *template.Renderer,
	logger *slog.Logger,
) *template.AdminService {
	return template.NewAdminService(
		repository.NewTemplateRepository(infra.postgresService.GetGormDB(), logger),
		templateRenderer,
		logger,
	)
}
