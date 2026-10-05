package deliverysql

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type DeliveryDB interface {
	dbx.Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// Texts는 domain 문자열 타입 목록을 ANY($n::text[])에 바인딩할 []string으로 바꾼다.
// 순서와 중복은 보존한다.
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

func ScanDeliveryRow(row pgx.CollectableRow) (domain.YouTubeNotificationDelivery, error) {
	var item domain.YouTubeNotificationDelivery

	err := row.Scan(
		&item.ID,
		&item.OutboxID,
		&item.RoomID,
		&item.Status,
		&item.AttemptCount,
		&item.NextAttemptAt,
		&item.CreatedAt,
		&item.LockedAt,
		&item.SentAt,
		&item.Error,
	)
	if err != nil {
		return item, fmt.Errorf("scan delivery row: %w", err)
	}

	return item, nil
}

const deleteBatchYield = 10 * time.Millisecond

func YieldBetweenDeleteBatches(ctx context.Context) error {
	timer := time.NewTimer(deleteBatchYield)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("yield between delete batches: %w", err)
		}

		return nil
	case <-timer.C:
		return nil
	}
}
