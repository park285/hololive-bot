package bootstrap

import (
	"fmt"
	"log/slog"

	"github.com/park285/iris-client-go/v3/valkeydedup"
	"github.com/park285/iris-client-go/v3/webhook"

	apiconfig "github.com/kapu/hololive-api/internal/config"
)

func BuildDurableBotWebhookHandler(
	appConfig *apiconfig.BotPlaneConfig,
	admitter webhook.MessageAdmitter,
	deps BotWebhookRuntimeDependencies,
	logger *slog.Logger,
) (*webhook.Handler, error) {
	nonceStore := valkeydedup.NewNonceStore(deps.Cache.GetClient())
	metrics := defaultWebhookMetrics()

	handler, err := webhook.NewDurableHandler(admitter,
		webhook.WithWebhookToken(appConfig.Iris.WebhookToken),
		webhook.WithWebhookLogger(logger),
		webhook.WithMetrics(metrics),
		webhook.WithNonceStore(nonceStore),
		webhook.WithMaxBodyBytes(appConfig.Webhook.MaxBodyBytes),
		webhook.WithNonceTimeout(appConfig.Webhook.DedupTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("durable webhook handler: %w", err)
	}

	metrics.BindSignatureDiagnostics(handler)

	return handler, nil
}
