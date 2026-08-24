package dispatchoutbox

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/pgxutil"
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
	EventID          int64
	EventKey         string
	RoomID           string
	DedupeKey        string
	ClaimKeys        []string
	DeliveryContext  []byte
	DispatchGroupKey string
	SendUnitKey      string
	ClientRequestID  string
	Status           Status
}

type eventBatchRow struct {
	EventKey    string         `json:"event_key"`
	PayloadHash string         `json:"payload_hash"`
	AlarmType   string         `json:"alarm_type"`
	ChannelID   string         `json:"channel_id"`
	StreamID    string         `json:"stream_id"`
	Category    string         `json:"category"`
	Payload     jsontext.Value `json:"payload"`
}

type deliveryBatchRow struct {
	EventID          int64          `json:"event_id"`
	RoomID           string         `json:"room_id"`
	DedupeKey        string         `json:"dedupe_key"`
	ClaimKeys        []string       `json:"claim_keys"`
	DeliveryContext  jsontext.Value `json:"delivery_context"`
	DispatchGroupKey string         `json:"dispatch_group_key"`
	SendUnitKey      string         `json:"send_unit_key"`
	ClientRequestID  string         `json:"client_request_id"`
	Status           string         `json:"status"`
}

func insertEvents(ctx context.Context, tx pgx.Tx, events []eventInsert) (result0 map[string]int64, result1 int, err error) {
	eventIDs := make(map[string]int64, len(events))
	if len(events) == 0 {
		return eventIDs, 0, nil
	}

	rows, _ := buildEventBatchRows(events)

	raw, err := jsonv2.Marshal(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("insert dispatch events: marshal batch: %w", err)
	}

	eventIDs, inserted, err := insertEventBatch(ctx, tx, raw)
	if err != nil {
		return nil, 0, fmt.Errorf("insert event batch: %w", err)
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
			Payload:     jsontext.Value(event.Payload).Clone(),
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
		return 0, fmt.Errorf("build delivery batch rows: %w", err)
	}

	raw, err := jsonv2.Marshal(rows)
	if err != nil {
		return 0, fmt.Errorf("insert dispatch deliveries: marshal batch: %w", err)
	}

	if unitErr := ensureSendUnits(ctx, tx, raw); unitErr != nil {
		return 0, fmt.Errorf("ensure send units: %w", unitErr)
	}

	selected, inserted, err := insertDeliveryBatch(ctx, tx, raw)
	if err != nil {
		return 0, fmt.Errorf("insert delivery batch: %w", err)
	}

	if selected != len(deliveries) {
		return 0, fmt.Errorf("insert dispatch deliveries: selected %d of %d rows", selected, len(deliveries))
	}

	return inserted, nil
}

func ensureSendUnits(ctx context.Context, tx pgx.Tx, raw []byte) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_insert_0179_01.sql"), jsonbRecordsetParam(raw)); err != nil {
		return fmt.Errorf("insert dispatch send units: %w", err)
	}

	return nil
}

func buildDeliveryBatchRows(deliveries []deliveryInsert) ([]deliveryBatchRow, error) {
	rows := make([]deliveryBatchRow, 0, len(deliveries))
	for i := range deliveries {
		delivery := &deliveries[i]
		if delivery.EventID <= 0 {
			return nil, fmt.Errorf("insert dispatch deliveries: missing event id for event_key=%s", delivery.EventKey)
		}

		rows = append(rows, deliveryBatchRow{
			EventID:          delivery.EventID,
			RoomID:           delivery.RoomID,
			DedupeKey:        delivery.DedupeKey,
			ClaimKeys:        delivery.ClaimKeys,
			DeliveryContext:  jsontext.Value(delivery.DeliveryContext).Clone(),
			DispatchGroupKey: delivery.DispatchGroupKey,
			SendUnitKey:      delivery.SendUnitKey,
			ClientRequestID:  delivery.ClientRequestID,
			Status:           string(delivery.Status),
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
	seenDeliveries := make(map[string]struct{}, len(envelopes))

	var collisions []eventCollision

	for i := range envelopes {
		collision, err := appendPreparedBatchRow(&envelopes[i], status, events, &deliveries, seenDeliveries, result)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("append prepared batch row: %w", err)
		}

		if collision != nil {
			collisions = append(collisions, *collision)
		}
	}

	if status == StatusPending {
		assignSendUnits(deliveries)
	}

	eventRows := make([]eventInsert, 0, len(events))
	for key := range events {
		eventRows = append(eventRows, events[key])
	}

	return eventRows, deliveries, collisions, nil
}

func appendPreparedBatchRow(
	envelope *domain.AlarmQueueEnvelope,
	status Status,
	events map[string]eventInsert,
	deliveries *[]deliveryInsert,
	seenDeliveries map[string]struct{},
	result *PublishBatchResult,
) (*eventCollision, error) {
	event, delivery, err := buildLedgerRows(envelope, status)
	if err != nil {
		return nil, fmt.Errorf("build ledger rows: %w", err)
	}

	collision := addPreparedEvent(events, &event, result)

	if _, exists := seenDeliveries[delivery.DedupeKey]; !exists {
		seenDeliveries[delivery.DedupeKey] = struct{}{}
		*deliveries = append(*deliveries, delivery)
	}

	return collision, nil
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
		err = finishDispatchBatch(ctx, tx, err, recover())
	}()

	collisions, deliveries, err := prepareBatchDeliveriesForInsert(ctx, tx, eventRows, deliveries, preflightCollisions, result, r.logger)
	if err != nil {
		return *result, fmt.Errorf("prepare batch deliveries for insert: %w", err)
	}

	insertedDeliveries, err := insertDeliveries(ctx, tx, deliveries)
	if err != nil {
		return *result, fmt.Errorf("insert deliveries: %w", err)
	}

	result.InsertedDeliveries = insertedDeliveries
	result.DuplicateDeliveries = result.RequestedDeliveries - insertedDeliveries

	if recordErr := recordEventCollisions(ctx, tx, collisions); recordErr != nil {
		err = recordErr
		return *result, err
	}

	if err = tx.Commit(ctx); err != nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: commit: %w", err)
	}

	return processedPublishBatchResult(result), nil
}

func finishDispatchBatch(ctx context.Context, tx pgx.Tx, err error, panicValue any) error {
	if panicValue != nil {
		rollbackErr := pgxutil.Rollback(ctx, tx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Default().Warn("dispatch batch rollback after panic failed", slog.Any("error", rollbackErr))
		}

		panic(panicValue)
	}

	if rollbackErr := rollbackDispatchBatchOnError(ctx, tx, err); rollbackErr != nil {
		return fmt.Errorf("rollback dispatch batch on error: %w", rollbackErr)
	}

	return nil
}
