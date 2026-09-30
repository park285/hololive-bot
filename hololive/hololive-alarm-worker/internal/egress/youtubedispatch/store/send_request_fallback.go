package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

// FreezeFallbackRequests는 grouped 요청의 확정 미전달 이후에만 호출한다.
// 모든 owner를 잠가 개별 요청 전체를 고정하므로 부분 commit의 membership 재해석을 방지한다.
func (s *TransitionStore) FreezeFallbackRequests(ctx context.Context, operation StartedOperation, requests []FrozenRequest) ([]FrozenRequest, error) {
	if len(requests) != operation.OwnerCount() || !operation.Valid() {
		return nil, errors.New("freeze fallback requests: incomplete operation")
	}

	frozen := make([]FrozenRequest, 0, len(requests))

	err := s.executeTx(ctx, "freeze fallback requests", func(tx dbx.Querier) error {
		groups := sortedStartedGroups(operation.groups)
		byID := make(map[int64]FrozenRequest, len(requests))

		for i := range requests {
			request := &requests[i]
			if len(request.MemberIDs) != 1 {
				return errors.New("freeze fallback requests: singleton required")
			}

			byID[request.MemberIDs[0]] = *request
		}

		for i := range groups {
			saved, err := freezeFallbackRequest(ctx, tx, groups[i], groups, byID)
			if err != nil {
				return err
			}

			frozen = append(frozen, saved)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("freeze youtube fallback requests: %w", err)
	}

	return frozen, nil
}

func freezeFallbackRequest(ctx context.Context, tx dbx.Querier, group startedLogicalGroup, groups []startedLogicalGroup, byID map[int64]FrozenRequest) (FrozenRequest, error) {
	request, ok := byID[group.ownerAfter.ID]
	if !ok || request.RoomID != group.ownerAfter.RoomID {
		return FrozenRequest{}, errors.New("freeze fallback requests: owner mismatch")
	}

	var bound string

	if err := tx.QueryRow(ctx, mustSQL("send_request_fallback_fence.sql"), group.ownerAfter.ID, group.ownerAfter.RowVersion).Scan(&bound); err != nil {
		return FrozenRequest{}, fmt.Errorf("freeze fallback requests: owner fence: %w", err)
	}

	if bound == "" || bound == request.BaseID {
		return FrozenRequest{}, errors.New("freeze fallback requests: invalid original request")
	}

	original, err := loadFrozenRequest(ctx, tx, bound)
	if err != nil {
		return FrozenRequest{}, err
	}

	if len(original.MemberIDs) != len(groups) {
		return FrozenRequest{}, errors.New("freeze fallback requests: original membership mismatch")
	}

	for i := range groups {
		if !slices.Contains(original.MemberIDs, groups[i].ownerAfter.ID) {
			return FrozenRequest{}, errors.New("freeze fallback requests: original member absent")
		}
	}

	saved, err := insertFrozenRequest(ctx, tx, request)
	if err != nil {
		return FrozenRequest{}, err
	}

	if _, bindErr := tx.Exec(ctx, mustSQL("send_request_fallback_bind.sql"), group.ownerAfter.ID, request.BaseID); bindErr != nil {
		return FrozenRequest{}, fmt.Errorf("freeze fallback requests: bind: %w", bindErr)
	}

	return saved, nil
}
