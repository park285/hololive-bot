package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/park285/iris-client-go/iris"
	"github.com/park285/iris-client-go/valkeydedup"
	"github.com/park285/iris-client-go/webhook"
)

func BuildDurableBotWebhookHandler(
	appConfig *settings.Config,
	admitter webhook.MessageAdmitter,
	deps BotWebhookRuntimeDependencies,
	logger *slog.Logger,
) (*webhook.Handler, error) {
	deduplicator := valkeydedup.New(deps.Cache.GetClient())
	metrics := defaultWebhookMetrics()
	handler, err := iris.NewDurableWebhookHandler(admitter,
		webhook.WithWebhookToken(appConfig.Iris.WebhookToken),
		webhook.WithWebhookLogger(logger),
		webhook.WithMetrics(metrics),
		webhook.WithDeduplicator(deduplicator),
		webhook.WithNonceCache(deduplicator),
		webhook.WithMaxBodyBytes(appConfig.Webhook.MaxBodyBytes),
		webhook.WithDedupTTL(appConfig.Webhook.DedupTTL),
		webhook.WithDedupTimeout(appConfig.Webhook.DedupTimeout),
	)
	if err != nil {
		return nil, err
	}
	metrics.BindSignatureDiagnostics(handler)
	return handler, nil
}
