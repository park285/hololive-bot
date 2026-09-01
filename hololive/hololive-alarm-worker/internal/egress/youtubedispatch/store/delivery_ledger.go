package store

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

const LedgerSchemaVersion = 1

// LedgerStatus is the monotonic terminal status stored for a logical delivery.
type LedgerStatus string

const (
	LedgerStatusSent        LedgerStatus = "SENT"
	LedgerStatusQuarantined LedgerStatus = "QUARANTINED"
)

// DeliveryLedgerRecord is a canonical logical delivery ledger row.
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

// LedgerWrite records one observed terminal physical delivery for a logical key.
type LedgerWrite struct {
	Key              ytcontentid.LogicalKey
	ObservedAt       time.Time
	SourceDeliveryID int64
}

// RecordDeliveryLedgerWrites applies monotonic logical delivery evidence in one batch.
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

	for i := range writes {
		kinds = append(kinds, string(writes[i].Key.Kind))
		logicalIDs = append(logicalIDs, writes[i].Key.LogicalID)
		roomIDs = append(roomIDs, writes[i].Key.RoomID)
		observedAts = append(observedAts, writes[i].ObservedAt)
		sourceDeliveryIDs = append(sourceDeliveryIDs, writes[i].SourceDeliveryID)
	}

	queryName := "delivery_ledger_record_sent.sql"

	if status == LedgerStatusQuarantined {
		queryName = "delivery_ledger_record_quarantined.sql"
	}

	var recorded []DeliveryLedgerRecord

	if err := deliverysql.SelectDeliverySQL(
		ctx,
		tx,
		&recorded,
		"record delivery ledger writes",
		mustSQL(queryName),
		kinds,
		logicalIDs,
		roomIDs,
		observedAts,
		sourceDeliveryIDs,
	); err != nil {
		return fmt.Errorf("upsert delivery ledger: %w", err)
	}

	if len(recorded) != len(writes) {
		return fmt.Errorf("delivery ledger result count mismatch: got %d want %d", len(recorded), len(writes))
	}

	for i := range recorded {
		if recorded[i].Status != status && (status != LedgerStatusQuarantined || recorded[i].Status != LedgerStatusSent) {
			return fmt.Errorf("delivery ledger state conflict at index %d: got %s want %s", i, recorded[i].Status, status)
		}
	}

	return nil
}
