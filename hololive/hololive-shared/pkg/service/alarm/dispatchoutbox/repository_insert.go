package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	json "github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

type eventBatchRow struct {
	EventKey    string          `json:"event_key"`
	PayloadHash string          `json:"payload_hash"`
	AlarmType   string          `json:"alarm_type"`
	ChannelID   string          `json:"channel_id"`
	StreamID    string          `json:"stream_id"`
	Category    string          `json:"category"`
	Payload     json.RawMessage `json:"payload"`
}

type deliveryBatchRow struct {
	EventID         int64           `json:"event_id"`
	RoomID          string          `json:"room_id"`
	DedupeKey       string          `json:"dedupe_key"`
	ClaimKeys       []string        `json:"claim_keys"`
	DeliveryContext json.RawMessage `json:"delivery_context"`
	Status          string          `json:"status"`
}

func insertEvents(ctx context.Context, tx pgx.Tx, events []eventInsert) (result0 map[string]int64, result1 int, err error) {
	eventIDs := make(map[string]int64, len(events))
	if len(events) == 0 {
		return eventIDs, 0, nil
	}
	rows, _ := buildEventBatchRows(events)
	raw, err := json.Marshal(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("insert dispatch events: marshal batch: %w", err)
	}
	eventIDs, inserted, err := insertEventBatch(ctx, tx, raw)
	if err != nil {
		return nil, 0, err
	}
	return eventIDs, inserted, nil
}

func buildEventBatchRows(events []eventInsert) (result0 []eventBatchRow, result1 map[string]string) {
	rows := make([]eventBatchRow, 0, len(events))
	expectedHashes := make(map[string]string, len(events))
	for _, event := range events {
		rows = append(rows, eventBatchRow{
			EventKey:    event.EventKey,
			PayloadHash: event.PayloadHash,
			AlarmType:   string(event.AlarmType),
			ChannelID:   event.ChannelID,
			StreamID:    event.StreamID,
			Category:    event.Category,
			Payload:     json.RawMessage(event.Payload),
		})
		expectedHashes[event.EventKey] = event.PayloadHash
	}
	return rows, expectedHashes
}

func insertEventBatch(ctx context.Context, tx pgx.Tx, raw []byte) (result0 map[string]int64, result1 int, err error) {
	rows, err := tx.Query(ctx, mustSQL("repository_insert_0092_01.sql"), jsonbRecordsetParam(raw))
	if err != nil {
		return nil, 0, fmt.Errorf("insert dispatch events: %w", err)
	}
	defer rows.Close()

	eventIDs := make(map[string]int64)
	for rows.Next() {
		var row insertedEventRow
		if err := rows.Scan(&row.ID, &row.EventKey, &row.PayloadHash); err != nil {
			return nil, 0, fmt.Errorf("insert dispatch events: scan: %w", err)
		}
		eventIDs[row.EventKey] = row.ID
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("insert dispatch events: rows: %w", err)
	}
	inserted := len(eventIDs)
	return eventIDs, inserted, nil
}

func truncateHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return h[:8] + "..."
}

func insertDeliveries(ctx context.Context, tx pgx.Tx, deliveries []deliveryInsert) (int, error) {
	if len(deliveries) == 0 {
		return 0, nil
	}
	rows, err := buildDeliveryBatchRows(deliveries)
	if err != nil {
		return 0, err
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return 0, fmt.Errorf("insert dispatch deliveries: marshal batch: %w", err)
	}
	selected, inserted, err := insertDeliveryBatch(ctx, tx, raw)
	if err != nil {
		return 0, err
	}
	if selected != len(deliveries) {
		return 0, fmt.Errorf("insert dispatch deliveries: selected %d of %d rows", selected, len(deliveries))
	}
	return inserted, nil
}

func buildDeliveryBatchRows(deliveries []deliveryInsert) ([]deliveryBatchRow, error) {
	rows := make([]deliveryBatchRow, 0, len(deliveries))
	for i := range deliveries {
		delivery := &deliveries[i]
		if delivery.EventID <= 0 {
			return nil, fmt.Errorf("insert dispatch deliveries: missing event id for event_key=%s", delivery.EventKey)
		}
		rows = append(rows, deliveryBatchRow{
			EventID:         delivery.EventID,
			RoomID:          delivery.RoomID,
			DedupeKey:       delivery.DedupeKey,
			ClaimKeys:       delivery.ClaimKeys,
			DeliveryContext: json.RawMessage(delivery.DeliveryContext),
			Status:          string(delivery.Status),
		})
	}
	return rows, nil
}

func insertDeliveryBatch(ctx context.Context, tx pgx.Tx, raw []byte) (selected, inserted int, err error) {
	err = tx.QueryRow(ctx, mustSQL("repository_insert_0183_02.sql"), jsonbRecordsetParam(raw)).Scan(&selected, &inserted)
	if err != nil {
		return 0, 0, fmt.Errorf("insert dispatch deliveries: %w", err)
	}
	return selected, inserted, nil
}

func prepareInsertBatchRows(envelopes []domain.AlarmQueueEnvelope, status Status, result *PublishBatchResult) ([]eventInsert, []deliveryInsert, []eventCollision, error) {
	events := make(map[string]eventInsert, len(envelopes))
	deliveries := make([]deliveryInsert, 0, len(envelopes))
	var collisions []eventCollision
	for i := range envelopes {
		event, delivery, err := buildLedgerRows(&envelopes[i], status)
		if err != nil {
			return nil, nil, nil, err
		}
		collision := addPreparedEvent(events, &event, result)
		if collision != nil {
			collisions = append(collisions, *collision)
		}
		deliveries = append(deliveries, delivery)
	}

	eventRows := make([]eventInsert, 0, len(events))
	for key := range events {
		eventRows = append(eventRows, events[key])
	}
	return eventRows, deliveries, collisions, nil
}

func addPreparedEvent(events map[string]eventInsert, event *eventInsert, result *PublishBatchResult) *eventCollision {
	existing, ok := events[event.EventKey]
	if ok && existing.PayloadHash != event.PayloadHash {
		result.HashConflictEvents++
		return &eventCollision{
			Event:               *event,
			ExistingPayloadHash: existing.PayloadHash,
		}
	}
	if !ok {
		events[event.EventKey] = *event
		result.RequestedEvents++
	}
	return nil
}

func (r *PgxRepository) insertPreparedBatch(ctx context.Context, eventRows []eventInsert, deliveries []deliveryInsert, preflightCollisions []eventCollision, result *PublishBatchResult) (publishResult PublishBatchResult, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: begin tx: %w", err)
	}
	defer func() {
		err = rollbackDispatchBatchOnError(ctx, tx, err)
	}()

	collisions, deliveries, err := prepareBatchDeliveriesForInsert(ctx, tx, eventRows, deliveries, preflightCollisions, result, r.logger)
	if err != nil {
		return *result, err
	}

	insertedDeliveries, err := insertDeliveries(ctx, tx, deliveries)
	if err != nil {
		return *result, err
	}
	result.InsertedDeliveries = insertedDeliveries
	result.DuplicateDeliveries = len(deliveries) - insertedDeliveries
	if recordErr := recordEventCollisions(ctx, tx, collisions); recordErr != nil {
		err = recordErr
		return *result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: commit: %w", err)
	}
	return processedPublishBatchResult(result), nil
}

func rollbackDispatchBatchOnError(ctx context.Context, tx pgx.Tx, err error) error {
	if err == nil {
		return nil
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
		return errors.Join(err, fmt.Errorf("rollback dispatch batch: %w", rollbackErr))
	}
	return err
}

func prepareBatchDeliveriesForInsert(
	ctx context.Context,
	tx pgx.Tx,
	eventRows []eventInsert,
	deliveries []deliveryInsert,
	preflightCollisions []eventCollision,
	result *PublishBatchResult,
	logger *slog.Logger,
) ([]eventCollision, []deliveryInsert, error) {
	eventIDs, collisions, err := insertPreparedEvents(ctx, tx, eventRows, result, logger)
	if err != nil {
		return nil, nil, err
	}
	collisions = append(preflightCollisions, collisions...)
	collisions = attachCollisionEventIDs(collisions, eventIDs)
	repointCollisionDeliveryEventIDs(eventIDs, collisions)
	assignDeliveryEventIDs(deliveries, eventIDs)
	return collisions, deliveries, nil
}

func insertPreparedEvents(ctx context.Context, tx pgx.Tx, eventRows []eventInsert, result *PublishBatchResult, logger *slog.Logger) (map[string]int64, []eventCollision, error) {
	existingRows, err := loadExistingEventRows(ctx, tx, eventRows)
	if err != nil {
		return nil, nil, err
	}
	classified := classifyEventPreflight(eventRows, existingRows)
	eventIDs := classified.EventIDs
	collisions := classified.Collisions

	insertedEventIDs, insertedEvents, err := insertEvents(ctx, tx, classified.InsertEvents)
	if err != nil {
		return nil, nil, err
	}
	mergeEventIDs(eventIDs, insertedEventIDs)

	missingEvents := missingInsertedEvents(classified.InsertEvents, insertedEventIDs)
	if len(missingEvents) > 0 {
		concurrentRows, err := loadExistingEventRows(ctx, tx, missingEvents)
		if err != nil {
			return nil, nil, err
		}
		concurrent := classifyEventPreflight(missingEvents, concurrentRows)
		if len(concurrent.InsertEvents) > 0 {
			return nil, nil, fmt.Errorf("insert dispatch events: found %d of %d inserted rows", len(insertedEventIDs), len(classified.InsertEvents))
		}
		mergeEventIDs(eventIDs, concurrent.EventIDs)
		collisions = append(collisions, concurrent.Collisions...)
	}

	logEventCollisions(logger, collisions)
	result.InsertedEvents = insertedEvents
	result.DuplicateEvents = len(eventRows) - insertedEvents - len(collisions)
	result.HashConflictEvents += len(collisions)
	return eventIDs, collisions, nil
}

func assignDeliveryEventIDs(deliveries []deliveryInsert, eventIDs map[string]int64) {
	for i := range deliveries {
		deliveries[i].EventID = eventIDs[deliveries[i].EventKey]
	}
}
