package dispatchoutbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *PgxRepository) findByDedupeKey(ctx context.Context, dedupeKey string) (*Record, error) {
	key := strings.TrimSpace(dedupeKey)
	if key == "" {
		return nil, fmt.Errorf("find dispatch delivery by dedupe key: dedupe key is empty")
	}
	row := r.pool.QueryRow(ctx, mustSQL("repository_claim_0029_01.sql"), []string{key}, key)
	record, err := scanDeliveryRecord(row)
	if err != nil {
		return nil, fmt.Errorf("find dispatch delivery by dedupe key: %w", err)
	}
	return record, nil
}

func (r *PgxRepository) ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
	if limit <= 0 {
		limit = 1
	}
	leaseSeconds := int(lease.Seconds())
	if leaseSeconds <= 0 {
		leaseSeconds = 60
	}
	rows, err := r.pool.Query(ctx, mustSQL("repository_claim_0053_02.sql"), limit, workerID, leaseSeconds)
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
		var event EventRecord
		var alarmType string
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
