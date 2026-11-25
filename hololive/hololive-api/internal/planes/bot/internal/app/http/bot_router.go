package apphttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/park285/iris-client-go/v6/webhook"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/readiness"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func ProvideBotRouter(
	ctx context.Context,
	appConfig *apiconfig.BotPlaneConfig,
	logger *slog.Logger,
	webhookHandler *webhook.Handler,
	readyProbe *sharedreadiness.Probe,
) (*gin.Engine, error) {
	if readyProbe == nil {
		return nil, errors.New("ready probe must not be nil")
	}

	out, err := sharedserver.NewRuntimeRouter(ctx, logger, &sharedserver.RuntimeRouterOptions{
		APIKey:                 appConfig.Server.APIKey,
		SkipLogPaths:           []string{"/l/*"},
		InternalReadyResponder: readiness.GinHandler(ctx, readyProbe),
		RegisterRoutes:         botRouteRegistrar(webhookHandler),
	})
	if err != nil {
		return nil, fmt.Errorf("runtime router: %w", err)
	}

	return out, nil
}

func botRouteRegistrar(webhookHandler *webhook.Handler) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		registerShortLinkRoutes(router)
		registerWebhookRoute(router, webhookHandler)

		return nil
	}
}

func registerWebhookRoute(router *gin.Engine, webhookHandler *webhook.Handler) {
	if webhookHandler == nil {
		return
	}

	router.POST("/webhook/iris", gin.WrapH(webhookHandler))
}
