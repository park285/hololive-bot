package bootstrap

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
	"github.com/park285/iris-client-go/iris"

	apphttp "github.com/kapu/hololive-kakao-bot-go/internal/app/http"
)

func BuildBotServer(
	ctx context.Context,
	cfg *config.Config,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
	alarmCRUD domain.AlarmCRUD,
	adminDeps *AdminServerDependencies,
	logger *slog.Logger,
) (*http.Server, error) {
	var (
		botRouter *gin.Engine
		err       error
	)

	if cfg.Bot.AdminEnabled {
		if adminDeps == nil || adminDeps.DomainHandlers == nil || adminDeps.AuthHandler == nil {
			return nil, errors.New("build bot server: admin routes enabled but dependencies are incomplete")
		}

		botRouter, err = apphttp.ProvideAPIRouter(
			ctx,
			cfg,
			logger,
			adminDeps.DomainHandlers,
			adminDeps.AuthHandler,
			webhookHandler,
			triggerHandler,
			adminDeps.Cache,
		)
		if err != nil {
			return nil, fmt.Errorf("build bot server: provide api router: %w", err)
		}
	} else {
		botRouter, err = apphttp.ProvideBotRouter(ctx, cfg, logger, webhookHandler, triggerHandler)
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

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	return sharedserver.NewH2CServer(addr, botRouter, "hololive-bot.http"), nil
}
