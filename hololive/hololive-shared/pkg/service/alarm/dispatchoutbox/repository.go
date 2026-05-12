package dispatchoutbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
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
		return r.findByDedupeKey(ctx, BuildDedupeKeyFromEnvelope(envelope))
	}
	return r.findByDedupeKey(ctx, BuildDedupeKeyFromEnvelope(envelope))
}

func (r *PgxRepository) InsertPending(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*Record, InsertResult, error) {
	result, err := r.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending})
	if err != nil {
		return nil, "", err
	}
	record, err := r.findByDedupeKey(ctx, BuildDedupeKeyFromEnvelope(envelope))
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

	events := make(map[string]eventInsert, len(input.Envelopes))
	deliveries := make([]deliveryInsert, 0, len(input.Envelopes))
	for _, envelope := range input.Envelopes {
		event, delivery, err := buildLedgerRows(envelope, status)
		if err != nil {
			return PublishBatchResult{}, err
		}
		if _, ok := events[event.EventKey]; !ok {
			events[event.EventKey] = event
			result.RequestedEvents++
		}
		deliveries = append(deliveries, delivery)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	eventIDs := make(map[string]int64, len(events))
	for _, event := range events {
		id, inserted, err := insertEvent(ctx, tx, event)
		if err != nil {
			return PublishBatchResult{}, err
		}
		if inserted {
			result.InsertedEvents++
		} else {
			result.DuplicateEvents++
		}
		eventIDs[event.EventKey] = id
	}
	for _, delivery := range deliveries {
		delivery.EventID = eventIDs[delivery.EventKey]
		inserted, err := insertDelivery(ctx, tx, delivery)
		if err != nil {
			return PublishBatchResult{}, err
		}
		if inserted {
			result.InsertedDeliveries++
		} else {
			result.DuplicateDeliveries++
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: commit: %w", err)
	}
	return result, nil
}

func insertEvent(ctx context.Context, tx pgx.Tx, event eventInsert) (int64, bool, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO alarm_dispatch_events (
			event_key, payload_hash, alarm_type, channel_id, stream_id, category,
			payload_schema_version, payload
		)
		VALUES ($1,$2,$3,$4,$5,$6,1,$7)
		ON CONFLICT (event_key) DO NOTHING
		RETURNING id`,
		event.EventKey, event.PayloadHash, string(event.AlarmType), event.ChannelID, event.StreamID, event.Category, event.Payload,
	).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !database.IsNoRows(err) {
		return 0, false, fmt.Errorf("insert dispatch event: %w", err)
	}
	var existingHash string
	err = tx.QueryRow(ctx, `
		SELECT id, payload_hash
		FROM alarm_dispatch_events
		WHERE event_key=$1`, event.EventKey).Scan(&id, &existingHash)
	if err != nil {
		return 0, false, fmt.Errorf("load existing dispatch event: %w", err)
	}
	if existingHash != event.PayloadHash {
		return 0, false, fmt.Errorf("dispatch event hash conflict: event_key=%s", event.EventKey)
	}
	return id, false, nil
}

func insertDelivery(ctx context.Context, tx pgx.Tx, delivery deliveryInsert) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO alarm_dispatch_deliveries (
			event_id, room_id, dedupe_key, claim_keys, delivery_context, status, next_attempt_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,NOW())
		ON CONFLICT (dedupe_key) DO NOTHING`,
		delivery.EventID, delivery.RoomID, delivery.DedupeKey, delivery.ClaimKeys, delivery.DeliveryContext, string(delivery.Status),
	)
	if err != nil {
		return false, fmt.Errorf("insert dispatch delivery: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PgxRepository) findByDedupeKey(ctx context.Context, dedupeKey string) (*Record, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, event_id, room_id, dedupe_key, claim_keys, delivery_context, status,
			attempt_count, next_attempt_at, locked_by, locked_at, lock_expires_at,
			sending_started_at, sent_at, dlq_at, quarantined_at, cancelled_at,
			last_error, created_at, updated_at
		FROM alarm_dispatch_deliveries
		WHERE dedupe_key = $1`, dedupeKey)
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
	for _, update := range updates {
		tag, err := r.pool.Exec(ctx, `
			UPDATE alarm_dispatch_deliveries
			SET status='retry',
				attempt_count=$2,
				next_attempt_at=$3,
				locked_by=NULL,
				locked_at=NULL,
				lock_expires_at=NULL,
				last_error=$4,
				updated_at=NOW()
			WHERE id=$1
			  AND status='leased'
			  AND locked_by=$5
			  AND lock_expires_at > NOW()`, update.ID, update.AttemptCount, update.NextAttemptAt, update.Error, workerID)
		if err != nil {
			return fmt.Errorf("schedule dispatch delivery retry: %w", err)
		}
		if err := expectRowsAffected(tag.RowsAffected(), 1, "schedule dispatch delivery retry"); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgxRepository) MoveToDLQ(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, updates, StatusDLQ, workerID)
}

func (r *PgxRepository) Quarantine(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, updates, StatusQuarantined, workerID)
}

func (r *PgxRepository) ReleaseLeased(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET locked_by=NULL, locked_at=NULL, lock_expires_at=NULL, updated_at=NOW()
		WHERE id = ANY($1) AND status = 'leased'`, ids)
	if err != nil {
		return fmt.Errorf("release dispatch deliveries: %w", err)
	}
	return nil
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

func (r *PgxRepository) terminal(ctx context.Context, ids []int64, status Status, reason string, workerID string) error {
	if len(ids) == 0 {
		return nil
	}
	column := "sent_at"
	statusFilter := "status NOT IN ('sent','dlq','quarantined','cancelled')"
	switch status {
	case StatusDLQ:
		column = "dlq_at"
		statusFilter = "status = 'leased'"
	case StatusQuarantined:
		column = "quarantined_at"
		statusFilter = "status = 'sending'"
	case StatusCancelled:
		column = "cancelled_at"
	}
	query := fmt.Sprintf(`
		UPDATE alarm_dispatch_deliveries
		SET status=$2,
			%s=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error=CASE WHEN $3 = '' THEN last_error ELSE $3 END,
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND locked_by = $4
		  AND %s`, column, statusFilter)
	tag, err := r.pool.Exec(ctx, query, ids, string(status), reason, workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries terminal: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries terminal")
}

func (r *PgxRepository) terminalUpdates(ctx context.Context, updates []TerminalUpdate, status Status, workerID string) error {
	for _, update := range updates {
		if err := r.terminal(ctx, []int64{update.ID}, status, update.Error, workerID); err != nil {
			return err
		}
	}
	return nil
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

type eventInsert struct {
	EventKey    string
	PayloadHash string
	AlarmType   domain.AlarmType
	ChannelID   string
	StreamID    string
	Category    string
	Payload     []byte
}

type deliveryInsert struct {
	EventID         int64
	EventKey        string
	RoomID          string
	DedupeKey       string
	ClaimKeys       []string
	DeliveryContext []byte
	Status          Status
}

type eventPayloadEnvelope struct {
	Notification eventPayloadNotification `json:"notification"`
	Version      uint8                    `json:"version"`
}

type eventPayloadNotification struct {
	AlarmType                   domain.AlarmType `json:"alarm_type,omitempty"`
	Channel                     *domain.Channel  `json:"channel"`
	Stream                      *domain.Stream   `json:"stream"`
	MinutesUntil                int              `json:"minutes_until"`
	ScheduleChangeMessage       string           `json:"schedule_change_message,omitempty"`
	ScheduleChangePreviousStart string           `json:"schedule_change_previous_start,omitempty"`
}

func buildLedgerRows(envelope domain.AlarmQueueEnvelope, status Status) (eventInsert, deliveryInsert, error) {
	input := EnvelopeDedupeInput(envelope)
	eventKey := BuildEventKey(input)
	dedupeInput := input
	if len(envelope.ClaimKeys) > 0 {
		dedupeInput.Category = envelope.ClaimKeys[len(envelope.ClaimKeys)-1]
	}
	dedupeKey := BuildDedupeKey(dedupeInput)
	payload, err := marshalEventPayload(envelope)
	if err != nil {
		return eventInsert{}, deliveryInsert{}, err
	}
	hash := sha256.Sum256(payload)
	deliveryContext, err := json.Marshal(deliveryContext{Users: envelope.Notification.Users})
	if err != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("build dispatch delivery context: %w", err)
	}
	return eventInsert{
			EventKey:    eventKey,
			PayloadHash: hex.EncodeToString(hash[:]),
			AlarmType:   input.AlarmType,
			ChannelID:   input.ChannelID,
			StreamID:    input.StreamID,
			Category:    eventCategory(input),
			Payload:     payload,
		}, deliveryInsert{
			EventKey:        eventKey,
			RoomID:          input.RoomID,
			DedupeKey:       dedupeKey,
			ClaimKeys:       envelope.ClaimKeys,
			DeliveryContext: deliveryContext,
			Status:          status,
		}, nil
}

func eventCategory(input DedupeInput) string {
	category := strings.TrimSpace(input.Category)
	if category != "" {
		return category
	}
	return strconv.Itoa(input.MinutesUntil)
}

func marshalEventPayload(envelope domain.AlarmQueueEnvelope) ([]byte, error) {
	payload := eventPayloadEnvelope{
		Notification: eventPayloadNotification{
			AlarmType:                   envelope.Notification.AlarmType,
			Channel:                     envelope.Notification.Channel,
			Stream:                      envelope.Notification.Stream,
			MinutesUntil:                envelope.Notification.MinutesUntil,
			ScheduleChangeMessage:       envelope.Notification.ScheduleChangeMessage,
			ScheduleChangePreviousStart: envelope.Notification.ScheduleChangePreviousStart,
		},
		Version: envelope.Version,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal dispatch event payload: %w", err)
	}
	return raw, nil
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
