package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const observationCollisionDetail = "observation identity collided with existing evidence"

func (r *Repository) completePublishTerminal(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	_ PublishBatchResult,
	hasCollision bool,
) error {
	if !hasCollision {
		if err := completeCollectionJob(ctx, tx, proof, ""); err != nil {
			return fmt.Errorf("complete collection job: %w", err)
		}

		return nil
	}

	diagnostic, err := contract.NewFailureDiagnostic(
		contract.ErrorObservationCollision,
		contract.ClassDataContract,
		observationCollisionDetail,
	)
	if err != nil {
		return fmt.Errorf("publish source observation batch: %w", err)
	}

	if err := completeCollectionJobWithError(ctx, tx, proof, diagnostic); err != nil {
		return fmt.Errorf("complete collection job with error: %w", err)
	}

	return nil
}

func deferPublishTerminal(deferInput DeferCollectionInput) leaseTerminalFunc {
	return func(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof, _ PublishBatchResult, _ bool) error {
		return deferCollectionJob(ctx, tx, proof, deferInput)
	}
}

func completeCollectionJobWithError(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	diagnostic contract.FailureDiagnostic,
) error {
	if err := diagnostic.ValidateFor(contract.TerminalCompleteError); err != nil {
		return fmt.Errorf("publish source observation batch: %w", err)
	}

	var jobKey string

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_job_complete_error_0081_81.sql"),
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ProjectionGeneration,
		proof.ScheduledFor,
		string(diagnostic.Code()),
		string(diagnostic.Class()),
		diagnostic.Detail(),
	).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCollectionFenceLost
	}

	if err != nil {
		return fmt.Errorf("publish source observation batch: complete collection job: %w", err)
	}

	return nil
}

func deferCollectionJob(
	ctx context.Context,
	tx dbx.Tx,
	proof *contract.LeaseProof,
	deferInput DeferCollectionInput,
) error {
	if err := deferInput.Validate(); err != nil {
		return fmt.Errorf("publish source observation batch: %w", err)
	}

	diagnostic := deferInput.Diagnostic()
	schedule := deferInput.Schedule()
	bounds := deferInput.Bounds()

	var jobKey string

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_job_defer_0082_82.sql"),
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ProjectionGeneration,
		proof.ScheduledFor,
		string(diagnostic.Code()),
		string(diagnostic.Class()),
		diagnostic.Detail(),
		string(schedule.Kind()),
		schedule.Delay().Milliseconds(),
		schedule.At(),
		bounds.Minimum.Milliseconds(),
		bounds.Maximum.Milliseconds(),
	).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCollectionFenceLost
	}

	if err != nil {
		return fmt.Errorf("publish source observation batch: defer collection job: %w", err)
	}

	return nil
}
