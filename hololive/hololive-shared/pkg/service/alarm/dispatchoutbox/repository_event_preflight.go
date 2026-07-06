package dispatchoutbox

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/jackc/pgx/v5"
)

type insertedEventRow struct {
	ID          int64
	EventKey    string
	PayloadHash string
}

type eventPreflightClassification struct {
	InsertEvents []eventInsert
	EventIDs     map[string]int64
	Collisions   []eventCollision
}

func loadExistingEventRows(ctx context.Context, tx pgx.Tx, events []eventInsert) (map[string]insertedEventRow, error) {
	existing := make(map[string]insertedEventRow)
	if len(events) == 0 {
		return existing, nil
	}
	keys := make([]string, 0, len(events))
	for i := range events {
		keys = append(keys, events[i].EventKey)
	}
	rows, err := tx.Query(ctx, mustSQL("repository_event_preflight_0033_01.sql"), keys)
	if err != nil {
		return nil, fmt.Errorf("preflight dispatch events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row insertedEventRow
		if err := rows.Scan(&row.ID, &row.EventKey, &row.PayloadHash); err != nil {
			return nil, fmt.Errorf("preflight dispatch events: scan: %w", err)
		}
		existing[row.EventKey] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("preflight dispatch events: rows: %w", err)
	}
	return existing, nil
}

func classifyEventPreflight(events []eventInsert, existing map[string]insertedEventRow) eventPreflightClassification {
	classified := eventPreflightClassification{
		InsertEvents: make([]eventInsert, 0, len(events)),
		EventIDs:     make(map[string]int64, len(events)),
	}
	for i := range events {
		event := &events[i]
		row, ok := existing[event.EventKey]
		if !ok {
			classified.InsertEvents = append(classified.InsertEvents, *event)
			continue
		}
		if row.PayloadHash == event.PayloadHash {
			classified.EventIDs[event.EventKey] = row.ID
			continue
		}
		classified.Collisions = append(classified.Collisions, eventCollision{
			Event:               *event,
			ExistingEventID:     row.ID,
			ExistingPayloadHash: row.PayloadHash,
		})
	}
	return classified
}

func mergeEventIDs(dst, src map[string]int64) {
	maps.Copy(dst, src)
}

func missingInsertedEvents(events []eventInsert, eventIDs map[string]int64) []eventInsert {
	missing := make([]eventInsert, 0)
	for _, event := range events {
		if _, ok := eventIDs[event.EventKey]; !ok {
			missing = append(missing, event)
		}
	}
	return missing
}

func logEventCollisions(logger *slog.Logger, collisions []eventCollision) {
	if logger == nil {
		return
	}
	for i := range collisions {
		collision := &collisions[i]
		logger.Warn("dispatch event hash conflict",
			slog.String("event_key", collision.Event.EventKey),
			slog.String("expected_hash", truncateHash(collision.Event.PayloadHash)),
			slog.String("actual_hash", truncateHash(collision.ExistingPayloadHash)),
		)
	}
}

func repointCollisionDeliveryEventIDs(eventIDs map[string]int64, collisions []eventCollision) {
	for i := range collisions {
		collision := &collisions[i]
		if collision.ExistingEventID > 0 {
			eventIDs[collision.Event.EventKey] = collision.ExistingEventID
		}
	}
}

func attachCollisionEventIDs(collisions []eventCollision, eventIDs map[string]int64) []eventCollision {
	if len(collisions) == 0 {
		return collisions
	}
	attached := make([]eventCollision, len(collisions))
	for i := range collisions {
		collision := &collisions[i]
		attached[i] = *collision
		if attached[i].ExistingEventID == 0 {
			attached[i].ExistingEventID = eventIDs[collision.Event.EventKey]
		}
	}
	return attached
}
