package bootstrap

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v5/webhook"

	apiconfig "github.com/kapu/hololive-api/internal/config"
)

func BuildDurableBotWebhookHandler(
	appConfig *apiconfig.BotPlaneConfig,
	admitter webhook.MessageAdmitter,
	pool *pgxpool.Pool,
	logger *slog.Logger,
) (*webhook.Handler, error) {
	if pool == nil {
		return nil, errors.New("webhook nonce postgres pool is nil")
	}

	metrics := defaultWebhookMetrics()

	handler, err := webhook.NewDurableHandler(admitter,
		webhook.WithWebhookToken(appConfig.Iris.WebhookToken),
		webhook.WithWebhookLogger(logger),
		webhook.WithMetrics(metrics),
		webhook.WithNonceStore(postgresNonceStore{pool: pool}),
		webhook.WithMaxBodyBytes(appConfig.Webhook.MaxBodyBytes),
		webhook.WithNonceTimeout(appConfig.Webhook.DedupTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("durable webhook handler: %w", err)
	}

	metrics.BindSignatureDiagnostics(handler)

	return handler, nil
}

type postgresNonceStore struct {
	pool *pgxpool.Pool
}

var _ webhook.SetOnceNonceStore = postgresNonceStore{}

func (postgresNonceStore) SetOnceNonce() {}

func (s postgresNonceStore) IsDuplicate(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	digest := sha256.Sum256([]byte(key))

	var reserved bool

	err := s.pool.QueryRow(ctx, reserveBotWebhookNonceSQL, digest[:], ttl.Microseconds()).Scan(&reserved)
	if err != nil {
		return false, fmt.Errorf("reserve webhook nonce: %w", err)
	}

	return !reserved, nil
}
