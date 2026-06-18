package dispatchoutbox

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	json "github.com/park285/shared-go/pkg/json"
)

type eventCollision struct {
	Event               eventInsert
	ExistingEventID     int64
	ExistingPayloadHash string
}

type eventCollisionBatchRow struct {
	ExistingEventID     *int64          `json:"existing_event_id"`
	EventKey            string          `json:"event_key"`
	ExistingPayloadHash string          `json:"existing_payload_hash"`
	IncomingPayloadHash string          `json:"incoming_payload_hash"`
	AlarmType           string          `json:"alarm_type"`
	ChannelID           string          `json:"channel_id"`
	StreamID            string          `json:"stream_id"`
	Category            string          `json:"category"`
	Payload             json.RawMessage `json:"payload"`
}

func recordEventCollisions(ctx context.Context, tx pgx.Tx, collisions []eventCollision) error {
	if len(collisions) == 0 {
		return nil
	}
	rows := buildEventCollisionBatchRows(collisions)
	raw, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("record dispatch event collisions: marshal batch: %w", err)
	}
	if _, err := tx.Exec(ctx, `
			WITH input AS (
				SELECT *
				FROM jsonb_to_recordset($1::jsonb) AS x(
					existing_event_id BIGINT,
					event_key TEXT,
					existing_payload_hash TEXT,
					incoming_payload_hash TEXT,
					alarm_type TEXT,
				channel_id TEXT,
				stream_id TEXT,
				category TEXT,
				payload JSONB
			)
		)
		INSERT INTO alarm_dispatch_event_collisions (
			existing_event_id, event_key, existing_payload_hash, incoming_payload_hash,
			alarm_type, channel_id, stream_id, category, payload_schema_version, payload
		)
		SELECT existing_event_id, event_key, existing_payload_hash, incoming_payload_hash,
			alarm_type::alarm_type, channel_id, stream_id, category, 1, payload
		FROM input
		ON CONFLICT (event_key, incoming_payload_hash) DO UPDATE SET
			existing_event_id = COALESCE(EXCLUDED.existing_event_id, alarm_dispatch_event_collisions.existing_event_id),
			existing_payload_hash = EXCLUDED.existing_payload_hash,
			alarm_type = EXCLUDED.alarm_type,
			channel_id = EXCLUDED.channel_id,
			stream_id = EXCLUDED.stream_id,
			category = EXCLUDED.category,
			payload_schema_version = EXCLUDED.payload_schema_version,
			payload = EXCLUDED.payload,
			status = 'detected',
			last_error = 'event_key payload_hash conflict',
			updated_at = NOW()`, jsonbRecordsetParam(raw)); err != nil {
		return fmt.Errorf("record dispatch event collisions: %w", err)
	}
	return nil
}

func buildEventCollisionBatchRows(collisions []eventCollision) []eventCollisionBatchRow {
	rows := make([]eventCollisionBatchRow, 0, len(collisions))
	for i := range collisions {
		collision := &collisions[i]
		var existingEventID *int64
		if collision.ExistingEventID > 0 {
			id := collision.ExistingEventID
			existingEventID = &id
		}
		rows = append(rows, eventCollisionBatchRow{
			ExistingEventID:     existingEventID,
			EventKey:            collision.Event.EventKey,
			ExistingPayloadHash: collision.ExistingPayloadHash,
			IncomingPayloadHash: collision.Event.PayloadHash,
			AlarmType:           string(collision.Event.AlarmType),
			ChannelID:           collision.Event.ChannelID,
			StreamID:            collision.Event.StreamID,
			Category:            collision.Event.Category,
			Payload:             json.RawMessage(collision.Event.Payload),
		})
	}
	return rows
}
