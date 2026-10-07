package store

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

var (
	recordSentLedgerSQL        = mustSQL("delivery_ledger_record_sent.sql")
	recordQuarantinedLedgerSQL = mustSQL("delivery_ledger_record_quarantined.sql")
)

// LedgerStatus는 논리 전송에 저장하는 단방향 종단 상태다.
type LedgerStatus string

const (
	LedgerStatusSent        LedgerStatus = "SENT"
	LedgerStatusQuarantined LedgerStatus = "QUARANTINED"
)

// DeliveryLedgerRecord는 정본 논리 전송 ledger 행이다.
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

// LedgerWrite는 논리 키에 대해 관측한 개별 전송의 종단 상태다.
type LedgerWrite struct {
	Key              ytcontentid.LogicalKey
	ObservedAt       time.Time
	SourceDeliveryID int64
}

// RecordDeliveryLedgerWrites는 논리 전송 증거를 되돌리지 않고 일괄 반영한다.
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

	query := recordSentLedgerSQL

	if status == LedgerStatusQuarantined {
		query = recordQuarantinedLedgerSQL
	}

	// 호출부는 오류만 받습니다. 기존 증거를 유지한 no-op도 성공이며,
	// 상태·필드 불변식은 DB CHECK, 배치의 중복 키는 위 검증이 보장합니다.
	if _, err := tx.Exec(ctx, query, kinds, logicalIDs, roomIDs, observedAts, sourceDeliveryIDs); err != nil {
		return fmt.Errorf("upsert delivery ledger: %w", err)
	}

	return nil
}
