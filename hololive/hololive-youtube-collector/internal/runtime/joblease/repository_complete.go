package joblease

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func (l *JobLease) CompleteCurrent(ctx context.Context) error {
	if l == nil || l.repository == nil {
		return fmt.Errorf("complete current collection job lease: %w", collection.ErrFenceLost)
	}

	if err := dbx.InPgxTx(ctx, l.repository.pool, func(tx dbx.Tx) error {
		return l.completeCurrentTx(ctx, tx)
	}); err != nil {
		return fmt.Errorf("complete current collection job lease: %w", err)
	}

	return nil
}

// completeCurrentTx는 tab이 없는 정상 empty COMPLETE의 terminal이다. Publish와 같은 순서로 guard(share) → lease
// (FOR UPDATE)를 잠근 뒤 새 snapshot에서 job membership을 판정한다. Checkpoint는 만들지 않는다.
// Not_before 같은 입장 조건은 판정하지 않는다.
func (l *JobLease) completeCurrentTx(ctx context.Context, tx dbx.Tx) error {
	// CURRENT 부재는 아래 membership 판정이 소유 손실 다음 순위로 보고한다.
	if _, _, err := lockProjectionGuard(ctx, tx); err != nil {
		return fmt.Errorf("lock projection guard: %w", err)
	}

	if err := lockActiveLease(ctx, tx, &l.proof); err != nil {
		return fmt.Errorf("lock active lease: %w", err)
	}

	if err := verifyLeaseMembership(ctx, tx, &l.proof, l.scope); err != nil {
		return fmt.Errorf("verify lease membership: %w", err)
	}

	if err := CompleteLease(ctx, tx, &l.proof); err != nil {
		return fmt.Errorf("complete current lease: %w", err)
	}

	return nil
}

// CompleteLease는 ACTIVE lease를 IDLE로 완료하고 다음 정상 슬롯으로 전진시키며 직전 실패 진단은 보존한다.
// 호출자는 같은 트랜잭션에서 lease 행을 먼저 잠그고 membership을 판정한다. 소유를 잃었으면 ErrFenceLost를 반환한다.
func CompleteLease(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof) error {
	var jobKey string

	err := tx.QueryRow(ctx, completeCurrentLeaseSQL, proof.JobKey, proof.OwnerInstance, proof.FenceEpoch,
		proof.ProjectionGeneration, proof.ScheduledFor).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("complete current collection job lease: %w", err)
	}

	return nil
}

// CompleteLeaseWithError는 CompleteLease처럼 다음 정상 슬롯으로 전진하면서 TerminalCompleteError 계약의 진단을 함께 기록한다.
// 잠금 전제와 소유 손실 결과는 CompleteLease와 같다.
func CompleteLeaseWithError(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof, diagnostic contract.FailureDiagnostic) error {
	if err := diagnostic.ValidateFor(contract.TerminalCompleteError); err != nil {
		return fmt.Errorf("complete current collection job lease with error: %w", err)
	}

	var jobKey string

	err := tx.QueryRow(ctx, completeCurrentLeaseWithErrorSQL, proof.JobKey, proof.OwnerInstance, proof.FenceEpoch,
		proof.ProjectionGeneration, proof.ScheduledFor,
		string(diagnostic.Code()), string(diagnostic.Class()), diagnostic.Detail(),
	).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("complete current collection job lease with error: %w", err)
	}

	return nil
}

func lockActiveLease(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof) error {
	var (
		failureCode, failureClass, failureDetail *string
		failureAt                                *time.Time
	)

	err := tx.QueryRow(
		ctx,
		lockActiveLeaseSQL,
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
	).Scan(&failureCode, &failureClass, &failureDetail, &failureAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return collection.ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("lock active collection job lease: %w", err)
	}

	return nil
}
