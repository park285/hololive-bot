package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

func rollbackDispatchBatchOnError(ctx context.Context, tx pgx.Tx, err error) error {
	if err == nil {
		return nil
	}

	if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
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
		return nil, nil, fmt.Errorf("insert prepared events: %w", err)
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
		return nil, nil, fmt.Errorf("load existing event rows: %w", err)
	}

	classified := classifyEventPreflight(eventRows, existingRows)
	eventIDs := classified.EventIDs
	collisions := classified.Collisions

	insertedEventIDs, insertedEvents, err := insertEvents(ctx, tx, classified.InsertEvents)
	if err != nil {
		return nil, nil, fmt.Errorf("insert events: %w", err)
	}

	mergeEventIDs(eventIDs, insertedEventIDs)

	missingEvents := missingInsertedEvents(classified.InsertEvents, insertedEventIDs)
	if len(missingEvents) > 0 {
		concurrentRows, err := loadExistingEventRows(ctx, tx, missingEvents)
		if err != nil {
			return nil, nil, fmt.Errorf("load existing event rows: %w", err)
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
