package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *PgxRepository) findByDedupeKey(ctx context.Context, dedupeKey string) (*Record, error) {
	key := strings.TrimSpace(dedupeKey)
	if key == "" {
		return nil, errors.New("find dispatch delivery by dedupe key: dedupe key is empty")
	}

	row := r.pool.QueryRow(ctx, mustSQL("repository_claim_0029_01.sql"), []string{key}, key)

	record, err := scanDeliveryRecord(row)
	if err != nil {
		return nil, fmt.Errorf("find dispatch delivery by dedupe key: %w", err)
	}

	return record, nil
}

// ClaimDue는 ready 멤버가 하나라도 있는 send unit을 골라 그 unit의 대기(pending·retry) 멤버 전원을 함께 lease한다.
// 멤버마다 따로 계산된 next_attempt_at이 어긋나도 발송 fence가 요구하는 전체 membership을 한 번에 넘긴다.
func (r *PgxRepository) ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
	if limit <= 0 {
		return nil, errors.New("claim due dispatch deliveries: limit must be positive")
	}

	leaseMS, err := leaseMilliseconds(lease)
	if err != nil {
		return nil, fmt.Errorf("claim due dispatch deliveries: lease: %w", err)
	}

	rows, err := r.pool.Query(ctx, sqlClaimDue, limit, workerID, leaseMS, maxDeliveriesPerSendUnit)
	if err != nil {
		return nil, fmt.Errorf("claim due dispatch deliveries: %w", err)
	}
	defer rows.Close()

	records := make([]*Record, 0, limit)

	for rows.Next() {
		record, err := scanDeliveryRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("claim due dispatch deliveries: scan: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim due dispatch deliveries: rows: %w", err)
	}

	return records, nil
}

func (r *PgxRepository) LoadEventsByID(ctx context.Context, eventIDs []int64) (map[int64]EventRecord, error) {
	events := make(map[int64]EventRecord, len(eventIDs))
	if len(eventIDs) == 0 {
		return events, nil
	}

	rows, err := r.pool.Query(ctx, mustSQL("repository_claim_0102_03.sql"), eventIDs)
	if err != nil {
		return nil, fmt.Errorf("load dispatch events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			event     EventRecord
			alarmType string
		)

		if err := rows.Scan(&event.ID, &event.EventKey, &event.PayloadHash, &alarmType, &event.ChannelID, &event.StreamID,
			&event.Category, &event.PayloadSchemaVersion, &event.Payload, &event.CreatedAt, &event.UpdatedAt); err != nil {
			return nil, fmt.Errorf("load dispatch events: scan: %w", err)
		}

		event.AlarmType = domain.AlarmType(alarmType)
		events[event.ID] = event
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load dispatch events: rows: %w", err)
	}

	return events, nil
}
