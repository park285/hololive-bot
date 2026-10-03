package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
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
		if err := completeCollectionJob(ctx, tx, proof); err != nil {
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

func (r *Repository) deferPublishTerminal(deferInput collection.DeferCollectionInput) leaseTerminalFunc {
	return func(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof, result PublishBatchResult, hasCollision bool) error {
		// 충돌은 불변 관측의 영구 결과입니다. PARTIAL이어도 같은 슬롯을 재시도하지 않고,
		// 독립 관측의 발행과 충돌 진단을 함께 확정한 뒤 다음 정상 슬롯으로 전진합니다.
		if hasCollision {
			return r.completePublishTerminal(ctx, tx, proof, result, hasCollision)
		}

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
		sqlJobCompleteError,
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
		return collection.ErrFenceLost
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
	deferInput collection.DeferCollectionInput,
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
		sqlJobDefer,
		proof.JobKey,
		proof.OwnerInstance,
		proof.FenceEpoch,
		proof.ProjectionGeneration,
		proof.ScheduledFor,
		string(diagnostic.Code()),
		string(diagnostic.Class()),
		diagnostic.Detail(),
		schedule.At(),
		bounds.Minimum.Milliseconds(),
		bounds.Maximum.Milliseconds(),
	).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("publish source observation batch: defer collection job: %w", err)
	}

	return nil
}
