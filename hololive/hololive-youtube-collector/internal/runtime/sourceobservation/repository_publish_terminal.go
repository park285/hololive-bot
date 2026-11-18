package sourceobservation

import (
	"context"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
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
		if err := joblease.CompleteLease(ctx, tx, proof); err != nil {
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

	if err := joblease.CompleteLeaseWithError(ctx, tx, proof, diagnostic); err != nil {
		return fmt.Errorf("complete collection job with error: %w", err)
	}

	return nil
}

func (r *Repository) deferPublishTerminal(deferInput collection.DeferCollectionInput) leaseTerminalFunc {
	return func(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof, result PublishBatchResult, hasCollision bool) error {
		if hasCollision {
			return r.completePublishTerminal(ctx, tx, proof, result, hasCollision)
		}

		if err := joblease.DeferLease(ctx, tx, proof, deferInput); err != nil {
			return fmt.Errorf("defer collection job: %w", err)
		}

		return nil
	}
}
