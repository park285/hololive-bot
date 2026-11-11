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

// 종료 후보가 없으면 문장 하나, 있으면 집합 갱신 하나가 더해진다.
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
		claimObservationsSQL,
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

	var terminal claimTerminalSet

	for rows.Next() {
		row, err := scanClaimRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claim row: %w", err)
		}

		if row.claimed {
			claims = append(claims, row.work)
			continue
		}

		if err := terminal.add(&row); err != nil {
			return nil, err
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim source observations: iterate rows: %w", err)
	}

	if err := terminal.deadLetter(ctx, tx); err != nil {
		return nil, err
	}

	return claims, nil
}

type claimRow struct {
	work                ClaimWork
	claimed             bool
	replayEpochRejected bool
}

func scanClaimRow(row pgx.Row) (claimRow, error) {
	var (
		out                          claimRow
		leaseToken, kind, subjectKey *string
	)

	if err := row.Scan(
		&out.work.ObservationID,
		&out.claimed,
		&leaseToken,
		&kind,
		&subjectKey,
		&out.work.AttemptCount,
		&out.replayEpochRejected,
	); err != nil {
		return claimRow{}, fmt.Errorf("claim source observations: scan row: %w", err)
	}

	if !out.claimed {
		return out, nil
	}

	if leaseToken == nil || kind == nil || subjectKey == nil {
		return claimRow{}, fmt.Errorf("claim source observations: claimed row %d lacks lease or subject", out.work.ObservationID)
	}

	out.work.LeaseToken = *leaseToken
	out.work.ObservationKind = contract.ObservationKind(*kind)
	out.work.SubjectKey = *subjectKey

	return out, nil
}

type claimTerminalSet struct {
	ids   []int64
	codes []string
}

func (s *claimTerminalSet) add(row *claimRow) error {
	code, ok := claimDeadLetterCode(row.replayEpochRejected, row.work.AttemptCount)
	if !ok {
		return fmt.Errorf("claim source observations: locked row %d has no terminal reason (attempt %d)",
			row.work.ObservationID, row.work.AttemptCount)
	}

	s.ids = append(s.ids, row.work.ObservationID)
	s.codes = append(s.codes, code)

	return nil
}

func (s *claimTerminalSet) deadLetter(ctx context.Context, tx dbx.Tx) error {
	if len(s.ids) == 0 {
		return nil
	}

	tag, err := tx.Exec(ctx, repositoryClaimDeadLetterSQL, s.ids, s.codes)
	if err != nil {
		return fmt.Errorf("claim source observations: dead letter terminal rows: %w", err)
	}

	if applied := tag.RowsAffected(); applied != int64(len(s.ids)) {
		return fmt.Errorf("claim source observations: dead lettered %d of %d locked terminal rows", applied, len(s.ids))
	}

	return nil
}

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

	if err := r.pool.QueryRow(ctx, repositoryReadySnapshotSQL, kindNames, maxAttempts).Scan(&values.Depth, &ageSeconds); err != nil {
		return workercontract.QueueValues{}, fmt.Errorf("snapshot source observation ready queue: %w", err)
	}

	values.OldestQueuedAge = time.Duration(ageSeconds * float64(time.Second))

	return values, nil
}
