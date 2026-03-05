package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	alarmsvc "github.com/kapu/hololive-shared/pkg/service/alarm"
)

// buildBotServer: Bot HTTP 서버를 구성합니다.
// - AdminEnabled=true: webhook + trigger + health + admin API
// - AdminEnabled=false: webhook + trigger + health
func buildBotServer(
	ctx context.Context,
	cfg *config.Config,
	webhookHandler *iris.WebhookHandler,
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
			return nil, fmt.Errorf("build bot server: admin routes enabled but dependencies are incomplete")
		}
		botRouter, err = ProvideAPIRouter(
			ctx,
			cfg,
			logger,
			adminDeps.domainHandlers,
			adminDeps.authHandler,
			webhookHandler,
			triggerHandler,
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
			return nil, fmt.Errorf("build bot server: internal alarm API requires API_SECRET_KEY")
		}
		alarmAPI := alarmsvc.NewAPIHandler(alarmCRUD, logger)
		internalAlarmGroup := botRouter.Group("")
		internalAlarmGroup.Use(sharedserver.APIKeyAuthMiddleware(cfg.Server.APIKey))
		alarmAPI.RegisterInternalRoutes(internalAlarmGroup)
	}

	addr := ProvideAPIAddr(cfg)
	return ProvideAPIServer(addr, botRouter), nil
}
