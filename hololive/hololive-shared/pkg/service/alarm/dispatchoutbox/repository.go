package dispatchoutbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type PgxRepository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewPgxRepository(postgres database.Client) *PgxRepository {
	if postgres == nil {
		return &PgxRepository{now: time.Now}
	}
	return &PgxRepository{pool: postgres.GetPool(), now: time.Now}
}

func NewPgxRepositoryFromPool(pool *pgxpool.Pool) *PgxRepository {
	return &PgxRepository{pool: pool, now: time.Now}
}

func (r *PgxRepository) InsertShadowed(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*Record, error) {
	result, err := r.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusShadowed})
	if err != nil {
		return nil, err
	}
	if result.InsertedDeliveries == 0 {
		return r.findByDedupeKeyAny(ctx, BuildDedupeKeyFromEnvelope(envelope), BuildLegacyDedupeKeyFromEnvelope(envelope))
	}
	return r.findByDedupeKeyAny(ctx, BuildDedupeKeyFromEnvelope(envelope), BuildLegacyDedupeKeyFromEnvelope(envelope))
}

func (r *PgxRepository) InsertPending(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*Record, InsertResult, error) {
	result, err := r.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending})
	if err != nil {
		return nil, "", err
	}
	record, err := r.findByDedupeKeyAny(ctx, BuildDedupeKeyFromEnvelope(envelope), BuildLegacyDedupeKeyFromEnvelope(envelope))
	if err != nil {
		return nil, "", err
	}
	if result.InsertedDeliveries > 0 {
		return record, Inserted, nil
	}
	switch record.Status {
	case StatusShadowed:
		return record, DuplicateShadowed, nil
	case StatusSent, StatusDLQ, StatusQuarantined, StatusCancelled:
		return record, DuplicateTerminal, nil
	default:
		return record, DuplicateActive, nil
	}
}

func (r *PgxRepository) InsertBatch(ctx context.Context, input PublishBatchInput) (PublishBatchResult, error) {
	if r == nil || r.pool == nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: postgres pool is nil")
	}
	status := input.Status
	if status == "" {
		status = StatusPending
	}
	if status != StatusPending && status != StatusShadowed {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: unsupported status %q", status)
	}
	result := PublishBatchResult{RequestedDeliveries: len(input.Envelopes)}
	if len(input.Envelopes) == 0 {
		return result, nil
	}

	eventRows, deliveries, result, err := prepareInsertBatchRows(input.Envelopes, status, result)
	if err != nil {
		return result, err
	}
	return r.insertPreparedBatch(ctx, eventRows, deliveries, result)
}

func prepareInsertBatchRows(envelopes []domain.AlarmQueueEnvelope, status Status, result PublishBatchResult) ([]eventInsert, []deliveryInsert, PublishBatchResult, error) {
	events := make(map[string]eventInsert, len(envelopes))
	deliveries := make([]deliveryInsert, 0, len(envelopes))
	for _, envelope := range envelopes {
		event, delivery, err := buildLedgerRows(envelope, status)
		if err != nil {
			return nil, nil, PublishBatchResult{}, err
		}
		result, err = addPreparedEvent(events, event, result)
		if err != nil {
			return nil, nil, result, err
		}
		deliveries = append(deliveries, delivery)
	}

	eventRows := make([]eventInsert, 0, len(events))
	for _, event := range events {
		eventRows = append(eventRows, event)
	}
	return eventRows, deliveries, result, nil
}

func addPreparedEvent(events map[string]eventInsert, event eventInsert, result PublishBatchResult) (PublishBatchResult, error) {
	existing, ok := events[event.EventKey]
	if ok && existing.PayloadHash != event.PayloadHash {
		result.HashConflictEvents++
		return result, fmt.Errorf("dispatch event hash conflict: event_key=%s", event.EventKey)
	}
	if !ok {
		events[event.EventKey] = event
		result.RequestedEvents++
	}
	return result, nil
}

func (r *PgxRepository) insertPreparedBatch(ctx context.Context, eventRows []eventInsert, deliveries []deliveryInsert, result PublishBatchResult) (PublishBatchResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: begin tx: %w", err)
	}
	defer rollbackDispatchBatchOnError(ctx, tx, &err)

	var eventIDs map[string]int64
	eventIDs, result, err = insertPreparedEvents(ctx, tx, eventRows, result)
	if err != nil {
		return result, err
	}
	assignDeliveryEventIDs(deliveries, eventIDs)

	insertedDeliveries, err := insertDeliveries(ctx, tx, deliveries)
	if err != nil {
		return result, err
	}
	result.InsertedDeliveries = insertedDeliveries
	result.DuplicateDeliveries = len(deliveries) - insertedDeliveries
	if err = tx.Commit(ctx); err != nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: commit: %w", err)
	}
	return processedPublishBatchResult(result), nil
}

func rollbackDispatchBatchOnError(ctx context.Context, tx pgx.Tx, err *error) {
	if *err != nil {
		_ = tx.Rollback(ctx)
	}
}

func insertPreparedEvents(ctx context.Context, tx pgx.Tx, eventRows []eventInsert, result PublishBatchResult) (map[string]int64, PublishBatchResult, error) {
	eventIDs, insertedEvents, err := insertEvents(ctx, tx, eventRows)
	if err != nil {
		if strings.Contains(err.Error(), "dispatch event hash conflict") {
			result.HashConflictEvents++
		}
		return nil, result, err
	}
	result.InsertedEvents = insertedEvents
	result.DuplicateEvents = len(eventRows) - insertedEvents
	return eventIDs, result, nil
}

func assignDeliveryEventIDs(deliveries []deliveryInsert, eventIDs map[string]int64) {
	for i := range deliveries {
		deliveries[i].EventID = eventIDs[deliveries[i].EventKey]
	}
}

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

func (r *PgxRepository) MarkSending(ctx context.Context, ids []int64, workerID string, extendLease time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	seconds := int(extendLease.Seconds())
	if seconds <= 0 {
		seconds = 60
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET status='sending',
			sending_started_at=NOW(),
			lock_expires_at=NOW() + ($2::INT * INTERVAL '1 second'),
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND status = 'leased'
		  AND locked_by = $3
		  AND lock_expires_at > NOW()`, ids, seconds, workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries sending: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries sending")
}

func (r *PgxRepository) MarkSent(ctx context.Context, ids []int64, workerID string) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET status='sent',
			sent_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND status = 'sending'
		  AND locked_by = $2
		  AND lock_expires_at > NOW()`, ids, workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries sent: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries sent")
}

func (r *PgxRepository) ScheduleRetry(ctx context.Context, updates []RetryUpdate, workerID string) error {
	if len(updates) == 0 {
		return nil
	}
	raw, err := json.Marshal(updates)
	if err != nil {
		return fmt.Errorf("schedule dispatch delivery retries: marshal batch: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		WITH input AS (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS x(
				id BIGINT,
				attempt_count INT,
				next_attempt_at TIMESTAMPTZ,
				error TEXT
			)
		)
			UPDATE alarm_dispatch_deliveries
			SET status='retry',
				attempt_count=input.attempt_count,
				next_attempt_at=input.next_attempt_at,
				locked_by=NULL,
				locked_at=NULL,
				lock_expires_at=NULL,
				last_error=input.error,
				updated_at=NOW()
			FROM input
			WHERE alarm_dispatch_deliveries.id=input.id
			  AND status='leased'
			  AND locked_by=$2
			  AND lock_expires_at > NOW()`, raw, workerID)
	if err != nil {
		return fmt.Errorf("schedule dispatch delivery retries: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(updates), "schedule dispatch delivery retries")
}

func (r *PgxRepository) MoveToDLQ(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, updates, StatusDLQ, workerID)
}

func (r *PgxRepository) Quarantine(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, updates, StatusQuarantined, workerID)
}

func (r *PgxRepository) ReleaseLeased(ctx context.Context, ids []int64, workerID string) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET status='retry',
			next_attempt_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error='lease released before external send',
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND status = 'leased'
		  AND locked_by = $2`, ids, workerID)
	if err != nil {
		return fmt.Errorf("release dispatch deliveries: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "release dispatch deliveries")
}

func (r *PgxRepository) RecoverExpiredLeased(ctx context.Context, limit int) (int, error) {
	return r.recoverWithQuery(ctx, `
		WITH picked AS (
			SELECT id FROM alarm_dispatch_deliveries
			WHERE status='leased' AND lock_expires_at < NOW()
			ORDER BY lock_expires_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE alarm_dispatch_deliveries d
		SET status='retry',
			next_attempt_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error='lease expired before external send',
			updated_at=NOW()
		FROM picked
		WHERE d.id = picked.id`, limit)
}

func (r *PgxRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	seconds := int(olderThan.Seconds())
	if seconds <= 0 {
		seconds = 300
	}
	return r.recoverWithQuery(ctx, `
		WITH picked AS (
			SELECT id FROM alarm_dispatch_deliveries
			WHERE status='sending'
			  AND sending_started_at < NOW() - ($2::INT * INTERVAL '1 second')
			ORDER BY sending_started_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE alarm_dispatch_deliveries d
		SET status='quarantined',
			quarantined_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error='stale sending; external send outcome unknown',
			updated_at=NOW()
		FROM picked
		WHERE d.id = picked.id`, limit, seconds)
}

func (r *PgxRepository) recoverWithQuery(ctx context.Context, query string, args ...any) (int, error) {
	if len(args) == 0 {
		args = append(args, 100)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("recover dispatch deliveries: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func expectRowsAffected(got int64, want int, action string) error {
	if got == int64(want) {
		return nil
	}
	return fmt.Errorf("%s: ownership mismatch: updated %d of %d rows", action, got, want)
}

func scanDeliveryRecord(row pgx.Row) (*Record, error) {
	var record Record
	var status string
	var lockedBy *string
	err := row.Scan(
		&record.ID,
		&record.EventID,
		&record.RoomID,
		&record.DedupeKey,
		&record.ClaimKeys,
		&record.DeliveryContext,
		&status,
		&record.AttemptCount,
		&record.NextAttemptAt,
		&lockedBy,
		&record.LockedAt,
		&record.LockExpiresAt,
		&record.SendingStartedAt,
		&record.SentAt,
		&record.DLQAt,
		&record.QuarantinedAt,
		&record.CancelledAt,
		&record.Error,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if lockedBy != nil {
		record.LockedBy = *lockedBy
	}
	record.Status = Status(status)
	return &record, nil
}

func idsFromEnvelopes(envelopes []domain.AlarmQueueEnvelope) []int64 {
	ids := make([]int64, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.DispatchOutboxID > 0 {
			ids = append(ids, envelope.DispatchOutboxID)
		}
	}
	return ids
}
