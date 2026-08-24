package joblease

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
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

func (l *JobLease) completeCurrentTx(ctx context.Context, tx dbx.Tx) error {
	if err := lockActiveLease(ctx, tx, &l.proof); err != nil {
		return fmt.Errorf("lock active lease: %w", err)
	}

	generation, err := lockAcquireProjection(ctx, tx)
	if err != nil {
		return fmt.Errorf("lock acquire projection: %w", err)
	}

	if err := l.verifyCurrentTargets(ctx, tx, generation); err != nil {
		return fmt.Errorf("verify current targets: %w", err)
	}

	if err := completeCurrentLease(ctx, tx, &l.proof); err != nil {
		return fmt.Errorf("complete current lease: %w", err)
	}

	return nil
}

func (l *JobLease) verifyCurrentTargets(ctx context.Context, tx dbx.Tx, generation int64) error {
	if generation != l.proof.ProjectionGeneration {
		return ErrProjectionStale
	}

	err := l.repository.verifyAcquireTargets(ctx, tx, &l.spec, l.contract, l.contract.CadenceKinds(), generation)
	if errors.Is(err, ErrInvalidJob) {
		return ErrTargetDisabled
	}

	if err != nil {
		return fmt.Errorf("verify acquire targets: %w", err)
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
