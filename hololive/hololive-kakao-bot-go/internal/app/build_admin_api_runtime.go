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
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	alarmsvc "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	apphttp "github.com/kapu/hololive-kakao-bot-go/internal/app/http"
)

func BuildAdminAPIRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*AdminAPIRuntime, error) {
	ctx, err := normalizeRuntimeBuildInputs(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	infra, err := appbootstrap.InitAdminAPIInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("build admin api runtime: init admin api infrastructure: %w", err)
	}

	adminDeps, err := buildAdminServerDependencies(ctx, cfg, infra, nil, logger)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build admin api runtime: admin dependencies: %w", err)
	}

	router, err := apphttp.ProvideAPIRouter(
		ctx,
		cfg,
		logger,
		adminDeps.DomainHandlers,
		adminDeps.AuthHandler,
		nil,
		nil,
		adminDeps.Cache,
	)
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build admin api runtime: provide api router: %w", err)
	}

	if infra.AlarmCRUD != nil {
		alarmAPI := alarmsvc.NewAPIHandler(infra.AlarmCRUD, logger)
		internalAlarm := router.Group("")
		internalAlarm.Use(middleware.APIKeyAuthMiddleware(cfg.Server.APIKey))
		alarmAPI.RegisterInternalRoutes(internalAlarm)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	return &AdminAPIRuntime{
		Config:     cfg,
		Logger:     logger,
		ServerAddr: addr,
		HttpServer: sharedserver.NewH2CServer(addr, router, "hololive-admin-api.http"),
		Managed:    lifecycle.NewManaged(infra.Cleanup),
	}, nil
}
