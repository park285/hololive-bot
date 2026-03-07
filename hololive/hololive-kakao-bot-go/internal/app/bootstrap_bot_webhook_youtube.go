package app

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
)

func buildBotWebhookHandler(
	cfg *config.Config,
	messageHandler iris.MessageHandler,
	deps botWebhookRuntimeDependencies,
	logger *slog.Logger,
) *iris.WebhookHandler {
	//nolint:contextcheck // worker goroutine은 task별 request context를 사용하므로 construction-time context 불필요
	return iris.NewWebhookHandler(cfg.Iris.WebhookToken, messageHandler, deps.cache.GetClient(), logger, iris.WebhookHandlerOptions{
		WorkerCount:    cfg.Webhook.WorkerCount,
		QueueSize:      cfg.Webhook.QueueSize,
		EnqueueTimeout: cfg.Webhook.EnqueueTimeout,
		HandlerTimeout: cfg.Webhook.HandlerTimeout,
		RequireHTTP2:   cfg.Webhook.RequireHTTP2,
	})
}
