package botruntime

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/park285/iris-client-go/iris"

	apphttp "github.com/kapu/hololive-api/internal/planes/bot/internal/app/http"
)

func ProvideBotRouter(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
) (*gin.Engine, error) {
	return apphttp.ProvideBotRouter(ctx, appConfig, logger, webhookHandler, triggerHandler)
}
