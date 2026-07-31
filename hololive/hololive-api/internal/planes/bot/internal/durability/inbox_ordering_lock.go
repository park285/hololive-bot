package durability

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (r *InboxRepository) beginLockedMessageTx(ctx context.Context, messageID string) (pgx.Tx, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin webhook transition: %w", err)
	}
	var orderingKey string
	err = tx.QueryRow(ctx, inboxOrderingKeyByMessageSQL, messageID).Scan(&orderingKey)
	if err != nil {
		return nil, errors.Join(err, rollbackInboxTx(ctx, tx))
	}
	if err := lockInboxOrderingKey(ctx, tx, orderingKey); err != nil {
		return nil, errors.Join(err, rollbackInboxTx(ctx, tx))
	}
	return tx, nil
}

func rollbackInboxTx(ctx context.Context, tx pgx.Tx) error {
	err := tx.Rollback(ctx)
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	return safeRepositoryError("rollback webhook transaction", err)
}

func lockInboxOrderingKey(ctx context.Context, tx pgx.Tx, orderingKey string) error {
	if _, err := tx.Exec(ctx, inboxOrderingKeyLockSQL, orderingKey); err != nil {
		return fmt.Errorf("lock webhook ordering key: %w", err)
	}
	return nil
}

func expiredInboxOrderingKeys(ctx context.Context, tx pgx.Tx, batchSize int32) ([]string, error) {
	rows, err := tx.Query(ctx, inboxReclaimExpiredKeysSQL, batchSize)
	if err != nil {
		return nil, fmt.Errorf("select expired webhook ordering keys: %w", err)
	}
	defer rows.Close()
	keys := make([]string, 0, batchSize)
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan expired webhook ordering key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}
