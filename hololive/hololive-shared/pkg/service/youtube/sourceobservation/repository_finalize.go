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

type ReconcileWrite func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error)

func (r *Repository) EnsureClaimBudget(
	ctx context.Context,
	claim Claim,
	transactionTimeout time.Duration,
) error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if err := validateClaim(claim); err != nil {
		return fmt.Errorf("validate claim: %w", err)
	}

	if transactionTimeout < time.Second || transactionTimeout > time.Minute {
		return errors.New("ensure source observation claim budget: transaction timeout is outside the accepted range")
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
		return ReconcileResult{}, fmt.Errorf("validate: %w", err)
	}

	if err := validateClaim(claim); err != nil {
		return ReconcileResult{}, fmt.Errorf("validate claim: %w", err)
	}

	if reconcile == nil {
		return ReconcileResult{}, errors.New("finalize source observation: reconcile writer is nil")
	}

	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (ReconcileResult, error) {
		return r.finalizeTx(ctx, tx, claim, reconcile)
	})
	if err != nil {
		return out, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return out, nil
}

func (r *Repository) finalizeTx(
	ctx context.Context,
	tx dbx.Tx,
	claim Claim,
	reconcile ReconcileWrite,
) (ReconcileResult, error) {
	observation, err := lockClaim(ctx, tx, claim)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("lock claim: %w", err)
	}

	rejected, rejection, rejectErr := rejectFinalizeObservation(ctx, tx, r.supported, &observation)
	if rejected {
		if rejectErr != nil {
			return rejection, fmt.Errorf("reject finalize observation: %w", rejectErr)
		}

		return rejection, nil
	}

	callbackObservation := observation

	result, err := reconcile(ctx, tx, &callbackObservation)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("finalize source observation: apply canonical domain write: %w", err)
	}

	if err := writeFinalizeApplications(ctx, tx, &observation, result.Applications); err != nil {
		return ReconcileResult{}, fmt.Errorf("write finalize applications: %w", err)
	}

	if err := completeFinalizeObservation(ctx, tx, claim, &observation); err != nil {
		return ReconcileResult{}, fmt.Errorf("complete finalize observation: %w", err)
	}

	result.EffectiveAt = observation.EffectiveAt
	result.SourceEventFallback = observation.SourceEventFallback

	return result, nil
}

func rejectFinalizeObservation(
	ctx context.Context,
	tx dbx.Tx,
	supported SupportedContractSet,
	observation *Observation,
) (bool, ReconcileResult, error) {
	if !supported.Supports(observation.ContractVersion()) {
		if err := deadLetterUnsupported(ctx, tx, observation); err != nil {
			return true, ReconcileResult{}, fmt.Errorf("dead letter unsupported: %w", err)
		}

		return true, unsupportedFinalizeResult(observation), nil
	}

	envelope := observation.Envelope()
	if _, err := envelope.ValidateAndCanonicalPayload(); err != nil {
		if dlErr := deadLetterTx(ctx, tx, DeadLetterInput{
			ObservationID: observation.ID,
			LeaseToken:    observation.LeaseToken,
			ErrorCode:     "invalid_payload",
			ErrorDetail:   boundedErrorDetail(err.Error()),
		}); dlErr != nil {
			return true, ReconcileResult{}, fmt.Errorf("dead letter tx: %w", dlErr)
		}

		return true, ReconcileResult{EffectiveAt: observation.EffectiveAt, SourceEventFallback: observation.SourceEventFallback}, nil
	}

	return false, ReconcileResult{}, nil
}

func deadLetterUnsupported(ctx context.Context, tx dbx.Tx, observation *Observation) error {
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
		return fmt.Errorf("dead letter tx: %w", err)
	}

	return nil
}

func unsupportedFinalizeResult(observation *Observation) ReconcileResult {
	return ReconcileResult{
		Unsupported:         true,
		EffectiveAt:         observation.EffectiveAt,
		SourceEventFallback: observation.SourceEventFallback,
	}
}

func writeFinalizeApplications(ctx context.Context, tx dbx.Tx, observation *Observation, applications []Application) error {
	if len(applications) > 1000 {
		return errors.New("finalize source observation: application count exceeds 1000")
	}

	for i := range applications {
		if err := writeFinalizeApplication(ctx, tx, observation, i, applications[i]); err != nil {
			return fmt.Errorf("write finalize application: %w", err)
		}
	}

	return nil
}

func writeFinalizeApplication(ctx context.Context, tx dbx.Tx, observation *Observation, index int, application Application) error {
	if err := validateText("application entity kind", application.EntityKind, 64); err != nil {
		return fmt.Errorf("finalize source observation: application %d: %w", index, err)
	}

	if err := validateText("application entity key", application.EntityKey, 256); err != nil {
		return fmt.Errorf("finalize source observation: application %d: %w", index, err)
	}

	if err := validateText("application decision", application.Decision, 128); err != nil {
		return fmt.Errorf("finalize source observation: application %d: %w", index, err)
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
		return fmt.Errorf("finalize source observation: insert application audit: %w", err)
	}

	return nil
}

func completeFinalizeObservation(ctx context.Context, tx dbx.Tx, claim Claim, observation *Observation) error {
	var observationID int64

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_complete_0015_15.sql"),
		observation.ID,
		observation.LeaseToken,
	).Scan(&observationID)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrClaimLost
	}

	if err != nil {
		return fmt.Errorf("finalize source observation: complete queue item: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_offset_upsert_0016_16.sql"),
		claim.ConsumerName,
		observation.ObservationKind,
		observation.ID,
		observation.EffectiveAt,
	); err != nil {
		return fmt.Errorf("finalize source observation: update consumer offset: %w", err)
	}

	return nil
}

func lockClaim(ctx context.Context, tx dbx.Tx, claim Claim) (Observation, error) {
	row := tx.QueryRow(
		ctx,
		mustSQL("repository_claim_lock_0013_13.sql"),
		claim.ObservationID,
		claim.LeaseToken,
	)
	observation, err := scanLockedObservation(row)

	if errors.Is(err, pgx.ErrNoRows) {
		return Observation{}, ErrClaimLost
	}

	if err != nil {
		return Observation{}, fmt.Errorf("finalize source observation: lock claim: %w", err)
	}

	return observation, nil
}

func scanLockedObservation(row pgx.Row) (Observation, error) {
	var (
		observation  Observation
		provider     string
		kind         string
		completeness string
		continuity   string
	)

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

func validateClaim(claim Claim) error {
	if err := validateText("consumer name", claim.ConsumerName, 128); err != nil {
		return fmt.Errorf("validate source observation claim: %w", err)
	}

	if claim.ObservationID <= 0 {
		return errors.New("validate source observation claim: observation id must be positive")
	}

	if !lowercaseHexToken(claim.LeaseToken) {
		return errors.New("validate source observation claim: lease token must be 64 lowercase hexadecimal characters")
	}

	return nil
}
