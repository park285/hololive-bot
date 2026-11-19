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

func (l *JobLease) completeCurrentTx(ctx context.Context, tx dbx.Tx) error {
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
