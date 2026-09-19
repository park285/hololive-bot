package dispatchops

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Requeue는 묶음 전체를 잠그고 리비전을 비교한 뒤 retry 전환과 감사를 원자적으로 커밋합니다.
// 기존 dedupe key, send unit, client_request_id, attempt_count와 오류 이력을 보존합니다.
// 직렬화 충돌이나 커밋 결과 불명은 자동 재실행하지 않습니다.
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

	rows, err := tx.Query(ctx, querySQL("requeue"), ids, request.OperatorID, request.Reason)
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

	// 연결 단절을 성공이나 실패로 추정하지 않습니다. 호출자는 재조회로 결과를 확인합니다.
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
