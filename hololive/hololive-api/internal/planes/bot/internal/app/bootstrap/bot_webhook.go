package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/iris-client-go/valkeydedup"
	"github.com/park285/iris-client-go/webhook"
)

func BuildBotWebhookHandler(
	appConfig *config.Config,
	messageHandler webhook.MessageHandler,
	deps BotWebhookRuntimeDependencies,
	webhookPool webhook.TaskPool,
	logger *slog.Logger,
) (*webhook.Handler, error) {
	return iris.NewWebhookHandler(messageHandler,
		webhook.WithWebhookToken(appConfig.Iris.WebhookToken),
		webhook.WithWebhookLogger(logger),
		valkeydedup.Option(deps.Cache.GetClient()),
		webhook.WithDedupMode(webhook.DedupModeAfterDecode),
		webhook.WithTaskPool(webhookPool),
		webhook.WithWorkerCount(appConfig.Webhook.WorkerCount),
		webhook.WithQueueSize(appConfig.Webhook.QueueSize),
		webhook.WithEnqueueTimeout(appConfig.Webhook.EnqueueTimeout),
		webhook.WithHandlerTimeout(appConfig.Webhook.HandlerTimeout),
		webhook.WithMaxBodyBytes(appConfig.Webhook.MaxBodyBytes),
		webhook.WithDedupTTL(appConfig.Webhook.DedupTTL),
		webhook.WithDedupTimeout(appConfig.Webhook.DedupTimeout),
	)
}
