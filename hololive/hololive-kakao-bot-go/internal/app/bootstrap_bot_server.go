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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
	alarmsvc "github.com/kapu/hololive-shared/pkg/service/alarm"
	iris "github.com/park285/iris-client-go/webhook"
)

// buildBotServer: Bot HTTP 서버를 구성합니다.
// - AdminEnabled=true: webhook + trigger + health + admin API
// - AdminEnabled=false: webhook + trigger + health.
func buildBotServer(
	ctx context.Context,
	cfg *config.Config,
	webhookHandler *iris.Handler,
	triggerHandler *sharedserver.TriggerHandler,
	alarmCRUD domain.AlarmCRUD,
	adminDeps *botAdminServerDependencies,
	logger *slog.Logger,
) (*http.Server, error) {
	var (
		botRouter *gin.Engine
		err       error
	)

	if cfg.Bot.AdminEnabled {
		if adminDeps == nil || adminDeps.domainHandlers == nil || adminDeps.authHandler == nil {
			return nil, errors.New("build bot server: admin routes enabled but dependencies are incomplete")
		}

		botRouter, err = ProvideAPIRouter(
			ctx,
			cfg,
			logger,
			adminDeps.domainHandlers,
			adminDeps.authHandler,
			webhookHandler,
			triggerHandler,
			adminDeps.cache,
		)
		if err != nil {
			return nil, fmt.Errorf("build bot server: provide api router: %w", err)
		}
	} else {
		botRouter, err = ProvideBotRouter(ctx, cfg, logger, webhookHandler, triggerHandler)
		if err != nil {
			return nil, fmt.Errorf("build bot server: provide bot router: %w", err)
		}
	}

	if alarmCRUD != nil {
		if strings.TrimSpace(cfg.Server.APIKey) == "" {
			return nil, errors.New("build bot server: internal alarm API requires API_SECRET_KEY")
		}

		alarmAPI := alarmsvc.NewAPIHandler(alarmCRUD, logger)
		internalAlarmGroup := botRouter.Group("")
		internalAlarmGroup.Use(middleware.APIKeyAuthMiddleware(cfg.Server.APIKey))
		alarmAPI.RegisterInternalRoutes(internalAlarmGroup)
	}

	addr := ProvideAPIAddr(cfg)

	return ProvideAPIServer(addr, botRouter), nil
}
