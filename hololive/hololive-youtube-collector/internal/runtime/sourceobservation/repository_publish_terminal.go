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

// completePublishTerminal·deferPublishTerminal의 lease 전이 SQL은 joblease가 소유하며,
// 발행 검증이 같은 트랜잭션에서 먼저 잡은 lease 행 잠금 아래에서 실행된다.
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
		// 충돌은 불변 관측의 영구 결과입니다. PARTIAL이어도 같은 슬롯을 재시도하지 않고,
		// 독립 관측의 발행과 충돌 진단을 함께 확정한 뒤 다음 정상 슬롯으로 전진합니다.
		if hasCollision {
			return r.completePublishTerminal(ctx, tx, proof, result, hasCollision)
		}

		if err := joblease.DeferLease(ctx, tx, proof, deferInput); err != nil {
			return fmt.Errorf("defer collection job: %w", err)
		}

		return nil
	}
}
