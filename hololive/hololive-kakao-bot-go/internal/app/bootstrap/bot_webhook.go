package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/iris-client-go/iris"
)

func BuildBotWebhookHandler(
	appConfig *config.Config,
	messageHandler iris.MessageHandler,
	deps BotWebhookRuntimeDependencies,
	webhookPool iris.TaskPool,
	logger *slog.Logger,
) (*iris.WebhookHandler, error) {
	return iris.NewWebhookHandler(messageHandler,
		iris.WithWebhookLogger(logger),
		iris.WithValkeyDedup(deps.Cache.GetClient()),
		iris.WithTaskPool(webhookPool),
		iris.WithWorkerCount(appConfig.Webhook.WorkerCount),
		iris.WithQueueSize(appConfig.Webhook.QueueSize),
		iris.WithEnqueueTimeout(appConfig.Webhook.EnqueueTimeout),
		iris.WithHandlerTimeout(appConfig.Webhook.HandlerTimeout),
		iris.WithMaxBodyBytes(appConfig.Webhook.MaxBodyBytes),
		iris.WithDedupTTL(appConfig.Webhook.DedupTTL),
		iris.WithDedupTimeout(appConfig.Webhook.DedupTimeout),
		iris.WithRequireHTTP2(appConfig.Webhook.RequireHTTP2),
	)
}
