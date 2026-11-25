package dispatchops

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
)

func (r *Repository) Requeue(ctx context.Context, id string, request RequeueRequest) (RequeueResult, error) {
	result := RequeueResult{IDs: []string{}}

	if validationErr := request.Validate(id); validationErr != nil {
		return result, validationErr
	}

	if availabilityErr := r.available(); availabilityErr != nil {
		return result, availabilityErr
	}

	numericID, err := ParseID(id)
	if err != nil {
		return result, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, fmt.Errorf("begin dispatch requeue: %w", err)
	}
	defer rollback(ctx, tx)

	group, err := loadGroup(ctx, tx, numericID, true)
	if err != nil {
		return result, fmt.Errorf("lock dispatch replay group: %w", conflictError(err))
	}

	if len(group) == 0 {
		return result, ErrNotFound
	}

	if validationErr := validateReplay(group, request); validationErr != nil {
		return result, validationErr
	}

	ids := make([]int64, 0, len(group))
	for index := range group {
		parsed, parseErr := ParseID(group[index].ID)
		if parseErr != nil {
			return result, fmt.Errorf("stored dispatch id: %w", parseErr)
		}

		ids = append(ids, parsed)
	}

	rows, err := pgxdb.Query(ctx, tx, requeueSQL, ids, request.OperatorID, request.Reason)
	if err != nil {
		return result, fmt.Errorf("update dispatch replay group: %w", conflictError(err))
	}

	updated, err := readUpdatedIDs(rows)
	if err != nil {
		return result, fmt.Errorf("read dispatch replay result: %w", conflictError(err))
	}

	if len(updated) != len(group) {
		return result, ErrConflict
	}

	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit dispatch requeue: %w", conflictError(err))
	}

	result.IDs = updated

	return result, nil
}

func readUpdatedIDs(rows pgx.Rows) ([]string, error) {
	defer rows.Close()

	ids := make([]string, 0, MaxReplaySize)

	for rows.Next() {
		var id string

		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan requeued dispatch id: %w", err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read requeued dispatch ids: %w", err)
	}

	return ids, nil
}
