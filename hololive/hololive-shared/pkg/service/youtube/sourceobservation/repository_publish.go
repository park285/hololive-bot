package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const maxCollectionLatency = 24 * time.Hour

func (r *Repository) Publish(
	ctx context.Context,
	envelope contract.Envelope,
	collectionLatency time.Duration,
) (PublishResult, error) {
	if r == nil || r.pool == nil {
		return PublishResult{}, ErrInvalidRepository
	}
	if collectionLatency < 0 || collectionLatency > maxCollectionLatency {
		return PublishResult{}, fmt.Errorf("publish source observation: collection latency must be between zero and %s", maxCollectionLatency)
	}
	canonicalPayload, err := envelope.ValidateAndCanonicalPayload()
	if err != nil {
		return PublishResult{}, fmt.Errorf("publish source observation: %w", err)
	}

	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (PublishResult, error) {
		return publishTx(ctx, tx, envelope, canonicalPayload, collectionLatency)
	})
}

func publishTx(
	ctx context.Context,
	tx dbx.Tx,
	envelope contract.Envelope,
	canonicalPayload []byte,
	collectionLatency time.Duration,
) (PublishResult, error) {
	fence, err := loadAuthority(ctx, tx, envelope.SourceKind, true)
	if err != nil {
		return PublishResult{}, err
	}
	if fence.Generation != envelope.Generation {
		return PublishResult{}, fmt.Errorf(
			"publish source observation: %w: expected=%d actual=%d",
			ErrStaleGeneration,
			envelope.Generation,
			fence.Generation,
		)
	}
	if err := lockSource(ctx, tx, envelope.SourceKind, envelope.SourceKey); err != nil {
		return PublishResult{}, err
	}

	previousKey, previousHash, found, err := loadCheckpointForUpdate(ctx, tx, envelope.SourceKind, envelope.SourceKey)
	if err != nil {
		return PublishResult{}, err
	}
	changed := !found || previousKey != envelope.ObservationKey || previousHash != envelope.PayloadSHA256
	result := PublishResult{Changed: changed, Fence: fence}
	if changed {
		result.ObservationID, result.Inserted, err = ensureObservation(ctx, tx, envelope, canonicalPayload)
		if err != nil {
			return PublishResult{}, err
		}
	}
	if err := upsertCheckpoint(ctx, tx, envelope, collectionLatency); err != nil {
		return PublishResult{}, err
	}
	return result, nil
}

func lockSource(ctx context.Context, tx dbx.Querier, sourceKind contract.SourceKind, sourceKey string) error {
	if _, err := tx.Exec(ctx, mustSQL("repository_source_lock_0011_11.sql"), sourceKind, sourceKey); err != nil {
		return fmt.Errorf("lock source observation checkpoint: %w", err)
	}
	return nil
}

func loadCheckpointForUpdate(
	ctx context.Context,
	tx dbx.Querier,
	sourceKind contract.SourceKind,
	sourceKey string,
) (string, string, bool, error) {
	var observationKey string
	var payloadHash string
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_checkpoint_lock_0003_03.sql"),
		sourceKind,
		sourceKey,
	).Scan(&observationKey, &payloadHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("load source collection checkpoint: %w", err)
	}
	return observationKey, payloadHash, true, nil
}

func ensureObservation(
	ctx context.Context,
	tx dbx.Querier,
	envelope contract.Envelope,
	canonicalPayload []byte,
) (int64, bool, error) {
	observationID, inserted, err := insertObservation(ctx, tx, envelope, canonicalPayload)
	if err != nil || inserted {
		return observationID, inserted, err
	}

	existingID, existingHash, err := loadObservationIdentity(ctx, tx, envelope)
	if err != nil {
		return 0, false, err
	}
	if existingHash != envelope.PayloadSHA256 {
		return 0, false, fmt.Errorf(
			"insert source observation: %w: source=%s key=%s observation=%s schema=%d",
			ErrObservationCollision,
			envelope.SourceKind,
			envelope.SourceKey,
			envelope.ObservationKey,
			envelope.SchemaVersion,
		)
	}
	return existingID, false, nil
}

func insertObservation(
	ctx context.Context,
	tx dbx.Querier,
	envelope contract.Envelope,
	canonicalPayload []byte,
) (int64, bool, error) {
	var observationID int64
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_observation_insert_0004_04.sql"),
		envelope.SourceKind,
		envelope.SourceKey,
		envelope.ObservationKey,
		envelope.SchemaVersion,
		envelope.Generation,
		envelope.ObservedAt,
		envelope.Completeness,
		envelope.Continuity,
		string(canonicalPayload),
		envelope.PayloadSHA256,
	).Scan(&observationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("insert source observation: %w", err)
	}
	return observationID, true, nil
}

func loadObservationIdentity(
	ctx context.Context,
	tx dbx.Querier,
	envelope contract.Envelope,
) (int64, string, error) {
	var observationID int64
	var payloadHash string
	err := tx.QueryRow(
		ctx,
		mustSQL("repository_observation_identity_0012_12.sql"),
		envelope.SourceKind,
		envelope.SourceKey,
		envelope.ObservationKey,
		envelope.SchemaVersion,
	).Scan(&observationID, &payloadHash)
	if err != nil {
		return 0, "", fmt.Errorf("load source observation identity: %w", err)
	}
	return observationID, payloadHash, nil
}

func upsertCheckpoint(
	ctx context.Context,
	tx dbx.Querier,
	envelope contract.Envelope,
	collectionLatency time.Duration,
) error {
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_checkpoint_upsert_0005_05.sql"),
		envelope.SourceKind,
		envelope.SourceKey,
		envelope.Generation,
		envelope.ObservationKey,
		envelope.PayloadSHA256,
		envelope.Completeness,
		envelope.Continuity,
		envelope.ObservedAt,
		collectionLatency.Milliseconds(),
	); err != nil {
		return fmt.Errorf("upsert source collection checkpoint: %w", err)
	}
	return nil
}
