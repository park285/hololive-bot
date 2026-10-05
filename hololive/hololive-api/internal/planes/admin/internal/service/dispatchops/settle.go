package dispatchops

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Settle는 실패 묶음의 상태와 감사를 원자적으로 변경하며 발송 큐에 등록하지 않습니다.
// 이미 발송 중이거나 완료·취소된 묶음 및 오래된 리비전은 거부합니다.
func (r *Repository) Settle(ctx context.Context, id string, request SettleRequest) (RequeueResult, error) {
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
		return result, fmt.Errorf("begin dispatch settlement: %w", err)
	}
	defer rollback(ctx, tx)

	group, err := loadGroup(ctx, tx, numericID, true)
	if err != nil {
		return result, fmt.Errorf("lock dispatch settlement group: %w", conflictError(err))
	}

	if len(group) == 0 {
		return result, ErrNotFound
	}

	if validationErr := validateSettlement(group, request); validationErr != nil {
		return result, validationErr
	}

	mutation, err := prepareSettlement(group, request.Action)
	if err != nil {
		return result, err
	}

	rows, err := tx.Query(ctx, querySQL("settle"), mutation.ids, request.OperatorID, request.Reason, request.Action,
		mutation.status, mutation.cancel, mutation.quarantineIDs)
	if err != nil {
		return result, fmt.Errorf("update dispatch settlement group: %w", conflictError(err))
	}

	updated, err := readUpdatedIDs(rows)
	if err != nil {
		return result, fmt.Errorf("read dispatch settlement result: %w", conflictError(err))
	}

	if len(updated) != len(group) {
		return result, ErrConflict
	}

	// 연결 단절을 성공이나 실패로 추정하지 않습니다. 호출자는 재조회로 결과를 확인합니다.
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit dispatch settlement: %w", conflictError(err))
	}

	result.IDs = updated

	return result, nil
}

type settlementMutation struct {
	ids, quarantineIDs []int64
	status             string
	cancel             bool
}

func prepareSettlement(group []Delivery, action string) (settlementMutation, error) {
	mutation := settlementMutation{
		ids:           make([]int64, 0, len(group)),
		quarantineIDs: make([]int64, 0, len(group)),
		status:        statusQuarantined,
		cancel:        action == "cancel",
	}
	if mutation.cancel {
		mutation.status = statusCanceled
	}

	for index := range group {
		item := &group[index]

		id, err := ParseID(item.ID)
		if err != nil {
			return settlementMutation{}, fmt.Errorf("stored dispatch id: %w", err)
		}

		mutation.ids = append(mutation.ids, id)
		if !mutation.cancel && item.Status != statusQuarantined {
			mutation.quarantineIDs = append(mutation.quarantineIDs, id)
		}
	}

	return mutation, nil
}
