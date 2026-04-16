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
	"os"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
)

func BuildAlarmWorkerRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*AlarmWorkerRuntime, error) {
	ctx, err := normalizeRuntimeBuildInputs(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	infra, err := appbootstrap.InitAlarmWorkerInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("build alarm worker runtime: init alarm worker infrastructure: %w", err)
	}

	scheduler, err := buildAlarmWorkerRuntimeScheduler(runtimeRoleWorker, cfg, infra, logger, os.Getenv(notificationSchedulerRoleEnv))
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: scheduler: %w", err)
	}

	router, err := sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
		APIKey: cfg.Server.APIKey,
	})
	if err != nil {
		infra.Cleanup()
		return nil, fmt.Errorf("build alarm worker runtime: router: %w", err)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	return &AlarmWorkerRuntime{
		Config:           cfg,
		Logger:           logger,
		Scheduler:        scheduler,
		ConfigSubscriber: BuildAlarmWorkerConfigSubscriber(ctx, infra.Cache, infra.AlarmCRUD, logger),
		ServerAddr:       addr,
		HttpServer:       sharedserver.NewH2CServer(addr, router, "hololive-alarm-worker.http"),
		Managed:          lifecycle.NewManaged(infra.Cleanup),
	}, nil
}
