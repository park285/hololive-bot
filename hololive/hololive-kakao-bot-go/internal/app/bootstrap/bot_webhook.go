package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/iris-client-go/iris"
)

func BuildBotWebhookHandler(
	cfg *config.Config,
	messageHandler iris.MessageHandler,
	deps BotWebhookRuntimeDependencies,
	logger *slog.Logger,
) (*iris.WebhookHandler, error) {
	return iris.NewWebhookHandler(messageHandler,
		iris.WithWebhookLogger(logger),
		iris.WithValkeyDedup(deps.Cache.GetClient()),
		iris.WithWorkerCount(cfg.Webhook.WorkerCount),
		iris.WithQueueSize(cfg.Webhook.QueueSize),
		iris.WithEnqueueTimeout(cfg.Webhook.EnqueueTimeout),
		iris.WithHandlerTimeout(cfg.Webhook.HandlerTimeout),
		iris.WithRequireHTTP2(cfg.Webhook.RequireHTTP2),
	)
}
