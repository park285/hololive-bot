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
	rows := buildEventCollisionBatchRows(dedupeEventCollisions(collisions))
	raw, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("record dispatch event collisions: marshal batch: %w", err)
	}
	if _, err := tx.Exec(ctx, mustSQL("repository_collision_0038_01.sql"), jsonbRecordsetParam(raw)); err != nil {
		return fmt.Errorf("record dispatch event collisions: %w", err)
	}
	return nil
}

func dedupeEventCollisions(collisions []eventCollision) []eventCollision {
	seen := make(map[string]struct{}, len(collisions))
	deduped := make([]eventCollision, 0, len(collisions))
	for i := range collisions {
		collision := &collisions[i]
		key := collision.Event.EventKey + "\x00" + collision.Event.PayloadHash
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, *collision)
	}
	return deduped
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
