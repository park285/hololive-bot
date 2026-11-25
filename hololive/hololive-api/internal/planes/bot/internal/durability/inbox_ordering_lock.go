package durability

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
)

func (r *InboxRepository) beginLockedMessageTx(ctx context.Context, messageID string) (pgx.Tx, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin webhook transition: %w", err)
	}

	if err := pgxdb.QueryRow(ctx, tx, inboxMessageOrderingLockSQL, messageID).Scan(nil); err != nil {
		return nil, errors.Join(fmt.Errorf("lock webhook message ordering key: %w", err), rollbackInboxTx(ctx, tx))
	}

	return tx, nil
}

func rollbackInboxTx(ctx context.Context, tx pgx.Tx) error {
	err := pgxdb.Rollback(ctx, tx)
	if err == nil {
		return nil
	}

	return safeRepositoryError("rollback webhook transaction", err)
}

func lockInboxOrderingKey(ctx context.Context, tx pgx.Tx, orderingKey string) error {
	if _, err := pgxdb.Exec(ctx, tx, inboxOrderingKeyLockSQL, orderingKey); err != nil {
		return fmt.Errorf("lock webhook ordering key: %w", err)
	}

	return nil
}

func lockInboxOrderingKeys(ctx context.Context, tx pgx.Tx, orderingKeys []string) error {
	if len(orderingKeys) == 0 {
		return nil
	}

	if tx == nil {
		return errors.Join(ErrInvalidArgument, errors.New("transaction must not be nil"))
	}

	if _, err := pgxdb.Exec(ctx, tx, inboxOrderingKeysLockSQL, orderingKeys); err != nil {
		return fmt.Errorf("lock webhook ordering keys: %w", err)
	}

	return nil
}

func expiredInboxOrderingKeys(ctx context.Context, tx pgx.Tx, batchSize int32) ([]string, error) {
	rows, err := pgxdb.Query(ctx, tx, inboxReclaimExpiredKeysSQL, batchSize)
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

	if err := rows.Err(); err != nil {
		return keys, fmt.Errorf("iterate inbox ordering rows: %w", err)
	}

	return keys, nil
}
