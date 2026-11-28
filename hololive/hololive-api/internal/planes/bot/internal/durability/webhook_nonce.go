package durability

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const WebhookNonceReplayWindow = 10 * time.Minute

type WebhookNonceStore struct {
	store *Store
}

func NewWebhookNonceStore(store *Store) WebhookNonceStore {
	return WebhookNonceStore{store: store}
}

func (WebhookNonceStore) SetOnceNonce() {}

func (s WebhookNonceStore) IsDuplicate(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := ensureStore(s.store); err != nil {
		return false, err
	}

	if ttl != WebhookNonceReplayWindow {
		return false, errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported webhook nonce replay window %s", ttl))
	}

	digest := sha256.Sum256([]byte(key))

	var reserved bool

	err := s.store.write(ctx, func(tx *sql.Tx, now int64) error {
		result, err := tx.ExecContext(ctx, webhookNonceReserveSQL, digest[:], now, ttl.Milliseconds())
		if err != nil {
			return fmt.Errorf("reserve webhook nonce: %w", err)
		}

		affected, err := rowsAffected(result)
		if err != nil {
			return err
		}

		reserved = affected == 1

		if _, err := tx.ExecContext(ctx, webhookNoncePruneSQL, now); err != nil {
			return fmt.Errorf("prune expired webhook nonces: %w", err)
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("check webhook nonce: %w", err)
	}

	return !reserved, nil
}
