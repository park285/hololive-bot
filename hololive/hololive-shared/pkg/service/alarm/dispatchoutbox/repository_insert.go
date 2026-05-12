package dispatchoutbox

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

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

func insertEvents(ctx context.Context, tx pgx.Tx, events []eventInsert) (map[string]int64, int, error) {
	eventIDs := make(map[string]int64, len(events))
	if len(events) == 0 {
		return eventIDs, 0, nil
	}
	rows := make([]eventBatchRow, 0, len(events))
	keys := make([]string, 0, len(events))
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
		keys = append(keys, event.EventKey)
		expectedHashes[event.EventKey] = event.PayloadHash
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("insert dispatch events: marshal batch: %w", err)
	}
	insertedRows, err := tx.Query(ctx, `
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
		ON CONFLICT (event_key) DO NOTHING
		RETURNING event_key`, raw)
	if err != nil {
		return nil, 0, fmt.Errorf("insert dispatch events: %w", err)
	}
	inserted := 0
	for insertedRows.Next() {
		inserted++
	}
	if err := insertedRows.Err(); err != nil {
		insertedRows.Close()
		return nil, 0, fmt.Errorf("insert dispatch events: rows: %w", err)
	}
	insertedRows.Close()

	existingRows, err := tx.Query(ctx, `
		SELECT id, event_key, payload_hash
		FROM alarm_dispatch_events
		WHERE event_key = ANY($1)`, keys)
	if err != nil {
		return nil, 0, fmt.Errorf("load dispatch event ids: %w", err)
	}
	defer existingRows.Close()
	for existingRows.Next() {
		var id int64
		var hash string
		var key string
		if err := existingRows.Scan(&id, &key, &hash); err != nil {
			return nil, 0, fmt.Errorf("load dispatch event ids: scan: %w", err)
		}
		if expectedHashes[key] != hash {
			return nil, 0, fmt.Errorf("dispatch event hash conflict: event_key=%s", key)
		}
		eventIDs[key] = id
	}
	if err := existingRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("load dispatch event ids: rows: %w", err)
	}
	if len(eventIDs) != len(events) {
		return nil, 0, fmt.Errorf("load dispatch event ids: found %d of %d rows", len(eventIDs), len(events))
	}
	return eventIDs, inserted, nil
}

func insertDeliveries(ctx context.Context, tx pgx.Tx, deliveries []deliveryInsert) (int, error) {
	if len(deliveries) == 0 {
		return 0, nil
	}
	rows := make([]deliveryBatchRow, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.EventID <= 0 {
			return 0, fmt.Errorf("insert dispatch deliveries: missing event id for event_key=%s", delivery.EventKey)
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
	raw, err := json.Marshal(rows)
	if err != nil {
		return 0, fmt.Errorf("insert dispatch deliveries: marshal batch: %w", err)
	}
	var selected int
	var inserted int
	err = tx.QueryRow(ctx, `
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
		SELECT (SELECT count(*) FROM normalized), (SELECT count(*) FROM inserted)`, raw).Scan(&selected, &inserted)
	if err != nil {
		return 0, fmt.Errorf("insert dispatch deliveries: %w", err)
	}
	if selected != len(deliveries) {
		return 0, fmt.Errorf("insert dispatch deliveries: selected %d of %d rows", selected, len(deliveries))
	}
	return inserted, nil
}
