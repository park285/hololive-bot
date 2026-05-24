package dispatchoutbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *PgxRepository) findByDedupeKeyAny(ctx context.Context, dedupeKeys ...string) (*Record, error) {
	keys := make([]string, 0, len(dedupeKeys))
	seen := make(map[string]struct{}, len(dedupeKeys))
	for _, key := range dedupeKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("find dispatch delivery by dedupe key: dedupe key is empty")
	}
	row := r.pool.QueryRow(ctx, `
		SELECT id, event_id, room_id, dedupe_key, claim_keys, delivery_context, status,
			attempt_count, next_attempt_at, locked_by, locked_at, lock_expires_at,
			sending_started_at, sent_at, dlq_at, quarantined_at, cancelled_at,
			last_error, created_at, updated_at
		FROM alarm_dispatch_deliveries
		WHERE dedupe_key = ANY($1)
		ORDER BY CASE WHEN dedupe_key = $2 THEN 0 ELSE 1 END, id ASC
		LIMIT 1`, keys, keys[0])
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
	rows, err := r.pool.Query(ctx, `
		WITH picked AS (
			SELECT id
			FROM alarm_dispatch_deliveries
			WHERE status IN ('pending', 'retry')
			  AND next_attempt_at <= NOW()
			ORDER BY next_attempt_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		), updated AS (
			UPDATE alarm_dispatch_deliveries d
			SET status = 'leased',
				locked_by = $2,
				locked_at = NOW(),
				lock_expires_at = NOW() + ($3::INT * INTERVAL '1 second'),
				updated_at = NOW()
			FROM picked
			WHERE d.id = picked.id
			RETURNING d.id, d.event_id, d.room_id, d.dedupe_key, d.claim_keys, d.delivery_context,
				d.status, d.attempt_count, d.next_attempt_at, d.locked_by, d.locked_at,
				d.lock_expires_at, d.sending_started_at, d.sent_at, d.dlq_at,
				d.quarantined_at, d.cancelled_at, d.last_error, d.created_at, d.updated_at
		)
		SELECT * FROM updated
		ORDER BY next_attempt_at ASC, id ASC`, limit, workerID, leaseSeconds)
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
	rows, err := r.pool.Query(ctx, `
		SELECT id, event_key, payload_hash, alarm_type, channel_id, stream_id, category,
			payload_schema_version, payload, created_at, updated_at
		FROM alarm_dispatch_events
		WHERE id = ANY($1)`, eventIDs)
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
