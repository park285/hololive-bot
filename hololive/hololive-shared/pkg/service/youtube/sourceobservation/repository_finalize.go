package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

type ReconcileWrite func(context.Context, dbx.Tx, Observation) (ReconcileResult, error)

func (r *Repository) EnsureClaimBudget(
	ctx context.Context,
	claim Claim,
	transactionTimeout time.Duration,
) error {
	if err := r.validate(); err != nil {
		return err
	}
	if err := validateClaim(claim); err != nil {
		return err
	}
	if transactionTimeout < time.Second || transactionTimeout > time.Minute {
		return fmt.Errorf("ensure source observation claim budget: transaction timeout is outside the accepted range")
	}
	var expiresAt time.Time
	err := r.pool.QueryRow(
		ctx,
		mustSQL("repository_claim_budget_0019_19.sql"),
		claim.ObservationID,
		claim.LeaseToken,
		(2 * transactionTimeout).Milliseconds(),
	).Scan(&expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrClaimLost
	}
	if err != nil {
		return fmt.Errorf("ensure source observation claim budget: %w", err)
	}
	return nil
}

func (r *Repository) Finalize(
	ctx context.Context,
	claim Claim,
	reconcile ReconcileWrite,
) (ReconcileResult, error) {
	if err := r.validate(); err != nil {
		return ReconcileResult{}, err
	}
	if err := validateClaim(claim); err != nil {
		return ReconcileResult{}, err
	}
	if reconcile == nil {
		return ReconcileResult{}, fmt.Errorf("finalize source observation: reconcile writer is nil")
	}
	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (ReconcileResult, error) {
		return r.finalizeTx(ctx, tx, claim, reconcile)
	})
}

func (r *Repository) finalizeTx(
	ctx context.Context,
	tx dbx.Tx,
	claim Claim,
	reconcile ReconcileWrite,
) (ReconcileResult, error) {
	observation, err := lockClaim(ctx, tx, claim)
	if err != nil {
		return ReconcileResult{}, err
	}
	if !r.supported.Supports(observation.ContractVersion()) {
		if err := deadLetterTx(ctx, tx, DeadLetterInput{
			ObservationID: observation.ID,
			LeaseToken:    observation.LeaseToken,
			ErrorCode:     "unsupported_contract",
			ErrorDetail: boundedErrorDetail(fmt.Sprintf(
				"provider=%s kind=%s schema=%d generation=%d",
				observation.Provider,
				observation.ObservationKind,
				observation.SchemaVersion,
				observation.ContractGeneration,
			)),
		}); err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{
			Unsupported:         true,
			EffectiveAt:         observation.EffectiveAt,
			SourceEventFallback: observation.SourceEventFallback,
		}, nil
	}
	if _, err := observation.Envelope().ValidateAndCanonicalPayload(); err != nil {
		if err := deadLetterTx(ctx, tx, DeadLetterInput{
			ObservationID: observation.ID,
			LeaseToken:    observation.LeaseToken,
			ErrorCode:     "invalid_payload",
			ErrorDetail:   boundedErrorDetail(err.Error()),
		}); err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{EffectiveAt: observation.EffectiveAt, SourceEventFallback: observation.SourceEventFallback}, nil
	}
	result, err := reconcile(ctx, tx, observation)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("finalize source observation: apply canonical domain write: %w", err)
	}
	if len(result.Applications) > 1000 {
		return ReconcileResult{}, fmt.Errorf("finalize source observation: application count exceeds 1000")
	}
	for i := range result.Applications {
		application := result.Applications[i]
		if err := validateText("application entity kind", application.EntityKind, 64); err != nil {
			return ReconcileResult{}, fmt.Errorf("finalize source observation: application %d: %w", i, err)
		}
		if err := validateText("application entity key", application.EntityKey, 256); err != nil {
			return ReconcileResult{}, fmt.Errorf("finalize source observation: application %d: %w", i, err)
		}
		if err := validateText("application decision", application.Decision, 128); err != nil {
			return ReconcileResult{}, fmt.Errorf("finalize source observation: application %d: %w", i, err)
		}
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_application_insert_0014_14.sql"),
			observation.ID,
			observation.Provider,
			observation.ObservationKind,
			observation.SubjectKey,
			observation.EvidenceSHA256,
			application.EntityKind,
			application.EntityKey,
			application.Decision,
			observation.EffectiveAt,
		); err != nil {
			return ReconcileResult{}, fmt.Errorf("finalize source observation: insert application audit: %w", err)
		}
	}
	var observationID int64
	err = tx.QueryRow(
		ctx,
		mustSQL("repository_complete_0015_15.sql"),
		observation.ID,
		observation.LeaseToken,
	).Scan(&observationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReconcileResult{}, ErrClaimLost
	}
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("finalize source observation: complete queue item: %w", err)
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_offset_upsert_0016_16.sql"),
		claim.ConsumerName,
		observation.ObservationKind,
		observation.ID,
		observation.EffectiveAt,
	); err != nil {
		return ReconcileResult{}, fmt.Errorf("finalize source observation: update consumer offset: %w", err)
	}
	result.EffectiveAt = observation.EffectiveAt
	result.SourceEventFallback = observation.SourceEventFallback
	return result, nil
}

func lockClaim(ctx context.Context, tx dbx.Tx, claim Claim) (Observation, error) {
	row := tx.QueryRow(
		ctx,
		mustSQL("repository_claim_lock_0013_13.sql"),
		claim.ObservationID,
		claim.LeaseToken,
	)
	observation, err := scanObservation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Observation{}, ErrClaimLost
	}
	if err != nil {
		return Observation{}, fmt.Errorf("finalize source observation: lock claim: %w", err)
	}
	return observation, nil
}

func validateClaim(claim Claim) error {
	if err := validateText("consumer name", claim.ConsumerName, 128); err != nil {
		return fmt.Errorf("validate source observation claim: %w", err)
	}
	if claim.ObservationID <= 0 {
		return fmt.Errorf("validate source observation claim: observation id must be positive")
	}
	if !lowercaseHexToken(claim.LeaseToken) {
		return fmt.Errorf("validate source observation claim: lease token must be 64 lowercase hexadecimal characters")
	}
	return nil
}
