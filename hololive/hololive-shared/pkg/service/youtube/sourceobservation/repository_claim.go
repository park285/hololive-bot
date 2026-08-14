package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func (r *Repository) ClaimBatch(ctx context.Context, options ClaimOptions) (ClaimedBatch, error) {
	if err := r.validate(); err != nil {
		return ClaimedBatch{}, err
	}
	if err := options.validate(); err != nil {
		return ClaimedBatch{}, err
	}
	leaseToken, err := newLeaseToken()
	if err != nil {
		return ClaimedBatch{}, fmt.Errorf("claim source observations: create lease token: %w", err)
	}
	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (ClaimedBatch, error) {
		observations, err := claimObservations(ctx, tx, options, leaseToken)
		if err != nil {
			return ClaimedBatch{}, err
		}
		return ClaimedBatch{ConsumerName: options.ConsumerName, Observations: observations}, nil
	})
}

func (r *Repository) ProbeClaim(ctx context.Context, options ClaimOptions) error {
	if err := r.validate(); err != nil {
		return err
	}
	if err := options.validate(); err != nil {
		return err
	}
	leaseToken, err := newLeaseToken()
	if err != nil {
		return fmt.Errorf("probe source observation claim: create lease token: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("probe source observation claim: begin: %w", err)
	}
	if _, err := claimObservations(ctx, tx, options, leaseToken); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("probe source observation claim: %w", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		return fmt.Errorf("probe source observation claim: rollback: %w", err)
	}
	return nil
}

func claimObservations(
	ctx context.Context,
	tx dbx.Tx,
	options ClaimOptions,
	leaseToken string,
) ([]Observation, error) {
	kinds := make([]string, len(options.Kinds))
	for i := range options.Kinds {
		kinds[i] = string(options.Kinds[i])
	}
	rows, err := tx.Query(
		ctx,
		mustSQL("repository_claim_0012_12.sql"),
		kinds,
		options.Limit,
		options.LeaseOwner,
		leaseToken,
		options.LeaseDuration.Milliseconds(),
		MaxAttempts,
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
	var provider string
	var kind string
	var completeness string
	var continuity string
	if err := row.Scan(
		&observation.ID,
		&provider,
		&kind,
		&observation.SubjectKey,
		&observation.ObservationKey,
		&observation.SchemaVersion,
		&observation.ContractGeneration,
		&observation.ScheduledFor,
		&observation.ObservedAt,
		&observation.SourceEventAt,
		&observation.ReceivedAt,
		&observation.ScopeSHA256,
		&completeness,
		&continuity,
		&observation.Payload,
		&observation.PayloadSHA256,
		&observation.EvidenceSHA256,
		&observation.CollectorInstance,
		&observation.JobKey,
		&observation.CollectionJobKind,
		&observation.FenceEpoch,
		&observation.ProjectionGeneration,
		&observation.AttemptCount,
		&observation.LeaseOwner,
		&observation.LeaseToken,
		&observation.LeaseExpiresAt,
	); err != nil {
		return Observation{}, fmt.Errorf("claim source observations: scan row: %w", err)
	}
	observation.Provider = contract.Provider(provider)
	observation.ObservationKind = contract.ObservationKind(kind)
	observation.Completeness = contract.Completeness(completeness)
	observation.Continuity = contract.Continuity(continuity)
	observation.EffectiveAt, observation.SourceEventFallback = contract.EffectiveAt(contract.ObservationClock{
		ObservationKind: observation.ObservationKind,
		ScheduledFor:    observation.ScheduledFor,
		SourceEventAt:   observation.SourceEventAt,
		ReceivedAt:      observation.ReceivedAt,
	}, contract.DefaultMaxSourceEventFutureSkew)
	return observation, nil
}
