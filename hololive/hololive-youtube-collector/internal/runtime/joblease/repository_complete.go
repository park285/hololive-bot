package joblease

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

func (l *JobLease) CompleteCurrent(ctx context.Context) error {
	if l == nil || l.repository == nil {
		return fmt.Errorf("complete current collection job lease: %w", ErrFenceLost)
	}

	if err := dbx.InPgxTx(ctx, l.repository.pool, func(tx dbx.Tx) error {
		return l.completeCurrentTx(ctx, tx)
	}); err != nil {
		return fmt.Errorf("in pgx tx: %w", err)
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

	if err := sourceobservation.VerifyLeaseMembership(ctx, tx, &l.proof, l.scope); err != nil {
		return fmt.Errorf("verify lease membership: %w", err)
	}

	if err := completeCurrentLease(ctx, tx, &l.proof); err != nil {
		return fmt.Errorf("complete current lease: %w", err)
	}

	return nil
}

func completeCurrentLease(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof) error {
	var jobKey string

	err := tx.QueryRow(ctx, mustSQL("repository_lease_complete_0144_11.sql"), proof.JobKey, proof.OwnerInstance, proof.FenceEpoch,
		proof.ProjectionGeneration, proof.ScheduledFor).Scan(&jobKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("complete current collection job lease: %w", err)
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
		mustSQL("repository_lease_failure_lock_0144_14.sql"),
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
	).Scan(&failureCode, &failureClass, &failureDetail, &failureAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFenceLost
	}

	if err != nil {
		return fmt.Errorf("lock active collection job lease: %w", err)
	}

	return nil
}
