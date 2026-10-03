package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func (r *Repository) ClaimBatch(ctx context.Context, options ClaimOptions) (ClaimedBatch, error) {
	if err := r.validate(); err != nil {
		return ClaimedBatch{}, fmt.Errorf("validate: %w", err)
	}

	if err := options.validate(); err != nil {
		return ClaimedBatch{}, fmt.Errorf("validate: %w", err)
	}

	leaseToken, err := newLeaseToken()
	if err != nil {
		return ClaimedBatch{}, fmt.Errorf("claim source observations: create lease token: %w", err)
	}

	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (ClaimedBatch, error) {
		claims, claimErr := claimObservations(ctx, tx, options, leaseToken)
		if claimErr != nil {
			return ClaimedBatch{}, fmt.Errorf("claim observations: %w", claimErr)
		}

		return ClaimedBatch{ConsumerName: options.ConsumerName, Claims: claims}, nil
	})
	if err != nil {
		return out, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return out, nil
}

func (r *Repository) ProbeClaim(ctx context.Context, options ClaimOptions) error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if err := options.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	leaseToken, err := newLeaseToken()
	if err != nil {
		return fmt.Errorf("probe source observation claim: create lease token: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("probe source observation claim: begin: %w", err)
	}

	if _, err := claimObservations(ctx, tx, options, leaseToken); err != nil {
		rollbackErr := tx.Rollback(ctx)
		if errors.Is(rollbackErr, pgx.ErrTxClosed) {
			rollbackErr = nil
		}

		return fmt.Errorf("probe source observation claim: %w", errors.Join(err, rollbackErr))
	}

	if err := tx.Rollback(ctx); err != nil {
		return fmt.Errorf("probe source observation claim: rollback: %w", err)
	}

	return nil
}

func claimObservations(
	ctx context.Context,
	tx dbx.Tx,
	options ClaimOptions,
	leaseToken string,
) ([]ClaimWork, error) {
	kinds := make([]string, len(options.Kinds))
	for i := range options.Kinds {
		kinds[i] = string(options.Kinds[i])
	}

	rows, err := tx.Query(
		ctx,
		mustSQL("repository_claim_0012_12.sql"),
		kinds,
		options.Limit,
		options.LeaseOwner,
		leaseToken,
		options.LeaseDuration.Milliseconds(),
		MaxAttempts,
	)
	if err != nil {
		return nil, fmt.Errorf("claim source observations: %w", err)
	}
	defer rows.Close()

	claims := make([]ClaimWork, 0, options.Limit)

	for rows.Next() {
		claim, err := scanClaimWork(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claim work: %w", err)
		}

		claims = append(claims, claim)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim source observations: iterate rows: %w", err)
	}

	return claims, nil
}

func scanClaimWork(row pgx.Row) (ClaimWork, error) {
	var (
		claim ClaimWork
		kind  string
	)

	if err := row.Scan(
		&claim.ObservationID,
		&claim.LeaseToken,
		&kind,
		&claim.SubjectKey,
	); err != nil {
		return ClaimWork{}, fmt.Errorf("claim source observations: scan row: %w", err)
	}

	claim.ObservationKind = contract.ObservationKind(kind)

	return claim, nil
}
