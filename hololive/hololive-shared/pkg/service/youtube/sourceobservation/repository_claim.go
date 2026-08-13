package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func (r *Repository) ClaimBatch(ctx context.Context, options ClaimOptions) (ClaimedBatch, error) {
	if r == nil || r.pool == nil {
		return ClaimedBatch{}, ErrInvalidRepository
	}
	if err := options.validate(); err != nil {
		return ClaimedBatch{}, err
	}
	leaseToken, err := newLeaseToken()
	if err != nil {
		return ClaimedBatch{}, fmt.Errorf("claim source observations: create lease token: %w", err)
	}

	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (ClaimedBatch, error) {
		return claimBatchTx(ctx, tx, options, leaseToken)
	})
}

func claimBatchTx(
	ctx context.Context,
	tx dbx.Tx,
	options ClaimOptions,
	leaseToken string,
) (ClaimedBatch, error) {
	fence, err := loadAuthority(ctx, tx, options.SourceKind, true)
	if err != nil {
		return ClaimedBatch{}, err
	}
	batch := ClaimedBatch{Fence: fence, ConsumerName: options.ConsumerName}
	if fence.Mode == contract.AuthorityModeLegacy {
		return batch, nil
	}

	batch.Observations, err = claimObservations(ctx, tx, options, leaseToken, fence.Generation)
	if err != nil {
		return ClaimedBatch{}, err
	}
	return batch, nil
}

func claimObservations(
	ctx context.Context,
	tx dbx.Querier,
	options ClaimOptions,
	leaseToken string,
	generation int64,
) ([]Observation, error) {
	rows, err := tx.Query(
		ctx,
		mustSQL("repository_claim_0006_06.sql"),
		options.SourceKind,
		options.Limit,
		options.LeaseOwner,
		leaseToken,
		options.LeaseDuration.Milliseconds(),
		MaxAttempts,
		generation,
	)
	if err != nil {
		return nil, fmt.Errorf("claim source observations: %w", err)
	}
	defer rows.Close()

	observations := make([]Observation, 0, options.Limit)
	for rows.Next() {
		observation, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim source observations: iterate rows: %w", err)
	}
	return observations, nil
}

func scanObservation(row pgx.Row) (Observation, error) {
	var observation Observation
	var sourceKind string
	var completeness string
	var continuity string
	if err := row.Scan(
		&observation.ID,
		&sourceKind,
		&observation.SourceKey,
		&observation.ObservationKey,
		&observation.SchemaVersion,
		&observation.Generation,
		&observation.ObservedAt,
		&completeness,
		&continuity,
		&observation.Payload,
		&observation.PayloadSHA256,
		&observation.AttemptCount,
		&observation.LeaseOwner,
		&observation.LeaseToken,
		&observation.LeaseExpiresAt,
	); err != nil {
		return Observation{}, fmt.Errorf("claim source observations: scan row: %w", err)
	}
	observation.SourceKind = contract.SourceKind(sourceKind)
	observation.Completeness = contract.Completeness(completeness)
	observation.Continuity = contract.Continuity(continuity)
	return observation, nil
}
