// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
)

type coreIntegrationServices struct {
	aclService        *acl.Service
	majorEventRepo    command.MajorEventRepository
	memberNewsService command.MemberNewsService
	commandBuilders   []bot.CommandBuilder
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
		acl.ParseACLMode(cfg.Kakao.ACLMode),
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
		commandBuilders:   []bot.CommandBuilder{},
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
