package bootstrap

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/iris-client-go/v6/webhook"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

func BuildDurableBotWebhookHandler(
	appConfig *apiconfig.BotPlaneConfig,
	admitter webhook.MessageAdmitter,
	store *durability.Store,
	logger *slog.Logger,
) (*webhook.Handler, error) {
	if store == nil {
		return nil, errors.New("webhook nonce durable store is nil")
	}

	metrics := defaultWebhookMetrics()

	handler, err := webhook.NewDurableHandler(admitter,
		webhook.WithWebhookToken(appConfig.Iris.WebhookToken),
		webhook.WithWebhookLogger(logger),
		webhook.WithMetrics(metrics),
		webhook.WithNonceStore(durability.NewWebhookNonceStore(store)),
		webhook.WithMaxBodyBytes(appConfig.Webhook.MaxBodyBytes),
		webhook.WithNonceTimeout(appConfig.Webhook.DedupTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("durable webhook handler: %w", err)
	}

	metrics.BindSignatureDiagnostics(handler)

	return handler, nil
}

var _ webhook.SetOnceNonceStore = durability.WebhookNonceStore{}
