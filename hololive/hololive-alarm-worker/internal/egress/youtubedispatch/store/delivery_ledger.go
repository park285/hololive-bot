package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

const (
	LedgerSchemaVersion         = 1
	compatibilityCleanupEnabled = false
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

type deliveryLedgerTarget struct {
	DeliveryID int64             `db:"delivery_id"`
	Kind       domain.OutboxKind `db:"kind"`
	ContentID  string            `db:"content_id"`
	Payload    string            `db:"payload"`
	RoomID     string            `db:"room_id"`
}

type deliveryLedgerWrite struct {
	Key              ytcontentid.LogicalKey
	ObservedAt       time.Time
	SourceDeliveryID int64
}

func (r *DeliveryRepository) CompatibilityCleanupReady(ctx context.Context) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("compatibility cleanup ready: db is nil")
	}

	var ready bool
	if err := r.db.QueryRow(ctx, mustSQL("delivery_ledger_cleanup_ready.sql"), LedgerSchemaVersion).Scan(&ready); err != nil {
		return false, fmt.Errorf("compatibility cleanup ready: %w", err)
	}

	return compatibilityCleanupEnabled && ready, nil
}

func recordSentLedgerForDeliveryIDs(
	ctx context.Context,
	tx dbx.Querier,
	deliveryIDs []int64,
	observedAt time.Time,
) error {
	writes, err := loadDeliveryLedgerWrites(ctx, tx, deliveryIDs, domain.OutboxStatusSent, observedAt)
	if err != nil {
		return fmt.Errorf("load sent ledger writes: %w", err)
	}

	if err := recordDeliveryLedgerWrites(ctx, tx, LedgerStatusSent, writes); err != nil {
		return fmt.Errorf("record sent ledger writes: %w", err)
	}

	return nil
}

func recordQuarantinedLedgerForDeliveryIDs(
	ctx context.Context,
	tx dbx.Querier,
	deliveryIDs []int64,
	observedAt time.Time,
) error {
	writes, err := loadDeliveryLedgerWrites(ctx, tx, deliveryIDs, DeliveryStatusQuarantined, observedAt)
	if err != nil {
		return fmt.Errorf("load quarantined ledger writes: %w", err)
	}

	if err := recordDeliveryLedgerWrites(ctx, tx, LedgerStatusQuarantined, writes); err != nil {
		return fmt.Errorf("record quarantined ledger writes: %w", err)
	}

	return nil
}

func loadDeliveryLedgerWrites(
	ctx context.Context,
	tx dbx.Querier,
	deliveryIDs []int64,
	status domain.OutboxStatus,
	observedAt time.Time,
) ([]deliveryLedgerWrite, error) {
	uniqueIDs := deliverysql.UniqueInt64s(deliveryIDs)
	if len(uniqueIDs) == 0 {
		return nil, nil
	}

	args := deliverysql.AppendDeliveryInt64Args(nil, uniqueIDs)
	args = append(args, status)

	var targets []deliveryLedgerTarget
	if err := deliverysql.SelectDeliverySQL(
		ctx,
		tx,
		&targets,
		"load delivery ledger targets",
		mustSQL("delivery_ledger_targets.sql")+deliverysql.DeliveryInClause("d.id", len(uniqueIDs))+" AND d.status = ? ORDER BY d.id ASC",
		args...,
	); err != nil {
		return nil, fmt.Errorf("select delivery ledger targets: %w", err)
	}

	if len(targets) != len(uniqueIDs) {
		return nil, fmt.Errorf("delivery ledger target count mismatch: got %d want %d", len(targets), len(uniqueIDs))
	}

	writesByKey := make(map[ytcontentid.LogicalKey]deliveryLedgerWrite, len(targets))
	for i := range targets {
		key, err := ytcontentid.ResolveDeliveryKey(
			targets[i].Kind,
			targets[i].ContentID,
			targets[i].Payload,
			targets[i].RoomID,
		)
		if err != nil {
			return nil, fmt.Errorf("resolve delivery ledger target at index %d: %w", i, err)
		}

		write := deliveryLedgerWrite{
			Key:              key,
			ObservedAt:       observedAt.UTC(),
			SourceDeliveryID: targets[i].DeliveryID,
		}
		if current, ok := writesByKey[key]; !ok || write.SourceDeliveryID < current.SourceDeliveryID {
			writesByKey[key] = write
		}
	}

	writes := slices.Collect(maps.Values(writesByKey))

	slices.SortFunc(writes, func(a, b deliveryLedgerWrite) int {
		if byKind := cmp.Compare(a.Key.Kind, b.Key.Kind); byKind != 0 {
			return byKind
		}
		if byLogicalID := cmp.Compare(a.Key.LogicalID, b.Key.LogicalID); byLogicalID != 0 {
			return byLogicalID
		}
		return cmp.Compare(a.Key.RoomID, b.Key.RoomID)
	})

	return writes, nil
}

func recordDeliveryLedgerWrites(
	ctx context.Context,
	tx dbx.Querier,
	status LedgerStatus,
	writes []deliveryLedgerWrite,
) error {
	if len(writes) == 0 {
		return nil
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
		if recorded[i].Status != status && !(status == LedgerStatusQuarantined && recorded[i].Status == LedgerStatusSent) {
			return fmt.Errorf("delivery ledger state conflict at index %d: got %s want %s", i, recorded[i].Status, status)
		}
	}

	return nil
}
