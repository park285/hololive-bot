package dispatchoutbox

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	json "github.com/park285/hololive-bot/shared-go/pkg/json"

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
	LegacyDedupeKey string
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
	LegacyDedupeKey string          `json:"legacy_dedupe_key"`
	ClaimKeys       []string        `json:"claim_keys"`
	DeliveryContext json.RawMessage `json:"delivery_context"`
	Status          string          `json:"status"`
}

func insertEvents(ctx context.Context, tx pgx.Tx, events []eventInsert, logger *slog.Logger) (map[string]int64, []string, int, error) {
	eventIDs := make(map[string]int64, len(events))
	if len(events) == 0 {
		return eventIDs, nil, 0, nil
	}
	rows, expectedHashes := buildEventBatchRows(events)
	raw, err := json.Marshal(rows)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("insert dispatch events: marshal batch: %w", err)
	}
	eventIDs, conflictKeys, inserted, err := insertEventBatch(ctx, tx, raw, expectedHashes, logger)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(eventIDs)+len(conflictKeys) != len(events) {
		return nil, nil, 0, fmt.Errorf("insert dispatch events: found %d+%d of %d rows", len(eventIDs), len(conflictKeys), len(events))
	}
	return eventIDs, conflictKeys, inserted, nil
}

func buildEventBatchRows(events []eventInsert) ([]eventBatchRow, map[string]string) {
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

func insertEventBatch(ctx context.Context, tx pgx.Tx, raw []byte, expectedHashes map[string]string, logger *slog.Logger) (map[string]int64, []string, int, error) {
	rows, err := tx.Query(ctx, `
		WITH input AS (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS x(
				event_key TEXT,
				payload_hash TEXT,
				alarm_type TEXT,
				channel_id TEXT,
				stream_id TEXT,
				category TEXT,
				payload JSONB
			)
		)
		INSERT INTO alarm_dispatch_events (
			event_key, payload_hash, alarm_type, channel_id, stream_id, category,
			payload_schema_version, payload
		)
		SELECT event_key, payload_hash, alarm_type::alarm_type, channel_id, stream_id, category, 1, payload
		FROM input
		ON CONFLICT (event_key) DO UPDATE SET updated_at = NOW()
		RETURNING id, event_key, payload_hash`, jsonbRecordsetParam(raw))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("insert dispatch events: %w", err)
	}
	defer rows.Close()

	eventIDs := make(map[string]int64, len(expectedHashes))
	var conflictKeys []string
	for rows.Next() {
		var id int64
		var key, hash string
		if err := rows.Scan(&id, &key, &hash); err != nil {
			return nil, nil, 0, fmt.Errorf("insert dispatch events: scan: %w", err)
		}
		expected, ok := expectedHashes[key]
		if ok && expected != hash {
			logger.Warn("dispatch event hash conflict, skipping",
				slog.String("event_key", key),
				slog.String("expected_hash", truncateHash(expected)),
				slog.String("actual_hash", truncateHash(hash)),
			)
			conflictKeys = append(conflictKeys, key)
			continue
		}
		eventIDs[key] = id
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("insert dispatch events: rows: %w", err)
	}
	inserted := len(eventIDs)
	return eventIDs, conflictKeys, inserted, nil
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
	for _, delivery := range deliveries {
		if delivery.EventID <= 0 {
			return nil, fmt.Errorf("insert dispatch deliveries: missing event id for event_key=%s", delivery.EventKey)
		}
		rows = append(rows, deliveryBatchRow{
			EventID:         delivery.EventID,
			RoomID:          delivery.RoomID,
			DedupeKey:       delivery.DedupeKey,
			LegacyDedupeKey: delivery.LegacyDedupeKey,
			ClaimKeys:       delivery.ClaimKeys,
			DeliveryContext: json.RawMessage(delivery.DeliveryContext),
			Status:          string(delivery.Status),
		})
	}
	return rows, nil
}

func insertDeliveryBatch(ctx context.Context, tx pgx.Tx, raw []byte) (int, int, error) {
	var selected int
	var inserted int
	err := tx.QueryRow(ctx, `
		WITH input AS (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS x(
				event_id BIGINT,
				room_id TEXT,
				dedupe_key TEXT,
				legacy_dedupe_key TEXT,
				claim_keys JSONB,
				delivery_context JSONB,
				status TEXT
			)
		), normalized AS (
			SELECT event_id,
				room_id,
				dedupe_key,
				legacy_dedupe_key,
				COALESCE(ARRAY(SELECT jsonb_array_elements_text(COALESCE(claim_keys, '[]'::jsonb))), ARRAY[]::TEXT[]) AS claim_keys,
				delivery_context,
				status
			FROM input
		), inserted AS (
		INSERT INTO alarm_dispatch_deliveries (
			event_id, room_id, dedupe_key, claim_keys, delivery_context, status, next_attempt_at
		)
			SELECT event_id, room_id, dedupe_key, claim_keys, delivery_context, status, NOW()
			FROM normalized
			WHERE NOT EXISTS (
				SELECT 1
				FROM alarm_dispatch_deliveries existing
				WHERE existing.dedupe_key = normalized.legacy_dedupe_key
			)
			ON CONFLICT (dedupe_key) DO NOTHING
			RETURNING dedupe_key
		)
		SELECT (SELECT count(*) FROM normalized), (SELECT count(*) FROM inserted)`, jsonbRecordsetParam(raw)).Scan(&selected, &inserted)
	if err != nil {
		return 0, 0, fmt.Errorf("insert dispatch deliveries: %w", err)
	}
	return selected, inserted, nil
}
