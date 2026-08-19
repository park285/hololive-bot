package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/iris-client-go/v2/valkeydedup"
	"github.com/park285/iris-client-go/v2/webhook"
)

func BuildDurableBotWebhookHandler(
	appConfig *settings.Config,
	admitter webhook.MessageAdmitter,
	deps BotWebhookRuntimeDependencies,
	logger *slog.Logger,
) (*webhook.Handler, error) {
	nonceStore := valkeydedup.NewNonceStore(deps.Cache.GetClient())
	metrics := defaultWebhookMetrics()
	handler, err := iris.NewDurableWebhookHandler(admitter,
		webhook.WithWebhookToken(appConfig.Iris.WebhookToken),
		webhook.WithWebhookLogger(logger),
		webhook.WithMetrics(metrics),
		webhook.WithNonceStore(nonceStore),
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
