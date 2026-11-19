package store

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

type LedgerStatus string

const (
	LedgerStatusSent        LedgerStatus = "SENT"
	LedgerStatusQuarantined LedgerStatus = "QUARANTINED"
)

type DeliveryLedgerRecord struct {
	Kind             domain.OutboxKind `db:"kind"`
	LogicalID        string            `db:"logical_id"`
	RoomID           string            `db:"room_id"`
	Status           LedgerStatus      `db:"status"`
	FirstRecordedAt  time.Time         `db:"first_recorded_at"`
	UpdatedAt        time.Time         `db:"updated_at"`
	SentAt           *time.Time        `db:"sent_at"`
	QuarantinedAt    *time.Time        `db:"quarantined_at"`
	SourceDeliveryID *int64            `db:"source_delivery_id"`
}

type LedgerWrite struct {
	Key              ytcontentid.LogicalKey
	ObservedAt       time.Time
	SourceDeliveryID int64
}

func RecordDeliveryLedgerWrites(
	ctx context.Context,
	tx dbx.Querier,
	status LedgerStatus,
	writes []LedgerWrite,
) error {
	if len(writes) == 0 {
		return nil
	}

	if status != LedgerStatusSent && status != LedgerStatusQuarantined {
		return fmt.Errorf("record delivery ledger writes: unsupported status %q", status)
	}

	kinds := make([]string, 0, len(writes))
	logicalIDs := make([]string, 0, len(writes))
	roomIDs := make([]string, 0, len(writes))
	observedAts := make([]time.Time, 0, len(writes))
	sourceDeliveryIDs := make([]int64, 0, len(writes))
	seen := make(map[ytcontentid.LogicalKey]struct{}, len(writes))

	for i := range writes {
		if _, duplicate := seen[writes[i].Key]; duplicate {
			return fmt.Errorf("record delivery ledger writes: duplicate logical key at index %d", i)
		}

		seen[writes[i].Key] = struct{}{}

		kinds = append(kinds, string(writes[i].Key.Kind))
		logicalIDs = append(logicalIDs, writes[i].Key.LogicalID)
		roomIDs = append(roomIDs, writes[i].Key.RoomID)
		observedAts = append(observedAts, writes[i].ObservedAt)
		sourceDeliveryIDs = append(sourceDeliveryIDs, writes[i].SourceDeliveryID)
	}

	query := deliveryLedgerRecordSentSQL

	if status == LedgerStatusQuarantined {
		query = deliveryLedgerRecordQuarantinedSQL
	}

	if _, err := tx.Exec(ctx, query, kinds, logicalIDs, roomIDs, observedAts, sourceDeliveryIDs); err != nil {
		return fmt.Errorf("upsert delivery ledger: %w", err)
	}

	return nil
}
