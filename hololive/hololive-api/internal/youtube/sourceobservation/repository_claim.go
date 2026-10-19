package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/workercontract"

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
		rollbackErr := pgxdb.Rollback(ctx, tx)

		return fmt.Errorf("probe source observation claim: %w", errors.Join(err, rollbackErr))
	}

	if err := pgxdb.Rollback(ctx, tx); err != nil {
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
		&claim.AttemptCount,
	); err != nil {
		return ClaimWork{}, fmt.Errorf("claim source observations: scan row: %w", err)
	}

	claim.ObservationKind = contract.ObservationKind(kind)

	return claim, nil
}

// ReadySnapshot은 replay epoch와 목록 선두를 포함한 durable claim 후보를 관찰한다.
// 다른 트랜잭션의 순간적인 행 잠금은 읽기 전용 snapshot에서 관찰하지 않는다.
func (r *Repository) ReadySnapshot(ctx context.Context, kinds []contract.ObservationKind, maxAttempts int) (workercontract.QueueValues, error) {
	if err := r.validate(); err != nil {
		return workercontract.QueueValues{}, fmt.Errorf("snapshot source observation queue: %w", err)
	}

	kindNames := make([]string, len(kinds))
	for i, kind := range kinds {
		kindNames[i] = string(kind)
	}

	var (
		values     workercontract.QueueValues
		ageSeconds float64
	)

	if err := r.pool.QueryRow(ctx, mustSQL("repository_ready_snapshot.sql"), kindNames, maxAttempts).Scan(&values.Depth, &ageSeconds); err != nil {
		return workercontract.QueueValues{}, fmt.Errorf("snapshot source observation ready queue: %w", err)
	}

	values.OldestQueuedAge = time.Duration(ageSeconds * float64(time.Second))

	return values, nil
}
