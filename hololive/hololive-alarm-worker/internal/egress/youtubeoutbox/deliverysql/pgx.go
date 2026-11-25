package deliverysql

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type DeliveryDB interface {
	pgxdb.Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func Texts[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}

	return out
}

func ScanOutboxRow(row pgx.CollectableRow) (domain.YouTubeNotificationOutbox, error) {
	var item domain.YouTubeNotificationOutbox

	err := row.Scan(
		&item.ID,
		&item.Kind,
		&item.ChannelID,
		&item.ContentID,
		&item.Payload,
		&item.Status,
		&item.AttemptCount,
		&item.NextAttemptAt,
		&item.CreatedAt,
		&item.LockedAt,
		&item.SentAt,
		&item.Error,
	)
	if err != nil {
		return item, fmt.Errorf("scan outbox row: %w", err)
	}

	return item, nil
}

const deleteBatchYield = 10 * time.Millisecond

func YieldBetweenDeleteBatches(ctx context.Context) error {
	if !retry.Sleep(ctx, deleteBatchYield) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("yield between delete batches: %w", err)
		}
	}

	return nil
}
