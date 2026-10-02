package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// FrozenRequest는 한 provider 호출의 불변 본문·수신 방·membership과 현재 ID 세대다.
type FrozenRequest struct {
	BaseID      string
	RoomID      string
	Message     string
	MessageHash string
	Route       string
	DedupeKeys  []string
	MemberIDs   []int64
	Generation  int
}

func (r FrozenRequest) ClientRequestID() (string, error) {
	if r.Generation == 0 {
		return r.BaseID, nil
	}

	id, err := iris.ReissuedClientRequestID(r.BaseID, r.Generation)
	if err != nil {
		return "", fmt.Errorf("frozen youtube request: reissued id: %w", err)
	}

	return id, nil
}

func scanFrozenRequest(row pgx.CollectableRow) (FrozenRequest, error) {
	var r FrozenRequest

	if err := row.Scan(&r.BaseID, &r.RoomID, &r.Message, &r.MessageHash, &r.Route, &r.DedupeKeys, &r.MemberIDs, &r.Generation); err != nil {
		return r, fmt.Errorf("scan frozen youtube request: %w", err)
	}

	sum := sha256.Sum256([]byte(r.Message))
	if r.MessageHash != hex.EncodeToString(sum[:]) {
		return r, errors.New("scan frozen youtube request: message hash mismatch")
	}

	return r, nil
}

func (s *TransitionStore) LoadFrozenRequests(ctx context.Context, deliveryIDs []int64) ([]FrozenRequest, error) {
	rows, err := s.db.Query(ctx, mustSQL("send_request_load.sql"), deliveryIDs)
	if err != nil {
		return nil, fmt.Errorf("load frozen youtube requests: query: %w", err)
	}
	defer rows.Close()

	requests, err := pgx.CollectRows(rows, scanFrozenRequest)
	if err != nil {
		return nil, fmt.Errorf("load frozen youtube requests: scan: %w", err)
	}

	return requests, nil
}

// FreezeRequest는 준비 fence를 잠근 상태에서 최초 요청만 저장한다. 전송 가능성이 있는
// legacy 행에는 현재 템플릿을 과거 요청의 증거로 사용하지 않는다.
func (s *TransitionStore) FreezeRequest(ctx context.Context, deliveries []domain.YouTubeNotificationDelivery, request FrozenRequest) (FrozenRequest, error) {
	var frozen FrozenRequest

	err := s.executeTx(ctx, "freeze request", func(tx dbx.Querier) error {
		var err error

		frozen, err = freezeRequestInTx(ctx, tx, deliveries, request)

		return err
	})
	if err != nil {
		return FrozenRequest{}, fmt.Errorf("freeze youtube request: %w", err)
	}

	return frozen, nil
}

func freezeRequestInTx(ctx context.Context, tx dbx.Querier, deliveries []domain.YouTubeNotificationDelivery, request FrozenRequest) (FrozenRequest, error) {
	ids := make([]int64, 0, len(deliveries))
	for i := range deliveries {
		ids = append(ids, deliveries[i].ID)
	}

	ids = uniqueSortedInt64s(ids)
	if len(ids) == 0 {
		return FrozenRequest{}, errors.New("freeze request: empty membership")
	}

	states, err := loadRequestDeliveryStates(ctx, tx, ids)
	if err != nil {
		return FrozenRequest{}, err
	}

	bound, err := validateRequestDeliveryStates(deliveries, states)
	if err != nil {
		return FrozenRequest{}, err
	}

	if bound != "" {
		return loadBoundFrozenRequest(ctx, tx, bound, ids)
	}

	request.MemberIDs = ids

	frozen, err := insertFrozenRequest(ctx, tx, request)
	if err != nil {
		return FrozenRequest{}, err
	}

	if err := bindFrozenRequest(ctx, tx, ids, request.BaseID); err != nil {
		return FrozenRequest{}, err
	}

	return frozen, nil
}

func loadRequestDeliveryStates(ctx context.Context, tx dbx.Querier, ids []int64) ([]requestDeliveryState, error) {
	rows, err := tx.Query(ctx, mustSQL("send_request_lock.sql"), ids)
	if err != nil {
		return nil, fmt.Errorf("freeze request: lock deliveries: %w", err)
	}

	states, err := pgx.CollectRows(rows, pgx.RowToStructByName[requestDeliveryState])
	if err != nil {
		return nil, fmt.Errorf("freeze request: read deliveries: %w", err)
	}

	return states, nil
}

func loadBoundFrozenRequest(ctx context.Context, tx dbx.Querier, bound string, ids []int64) (FrozenRequest, error) {
	frozen, err := loadFrozenRequest(ctx, tx, bound)
	if err != nil {
		return FrozenRequest{}, err
	}

	if !slices.Equal(uniqueSortedInt64s(frozen.MemberIDs), ids) {
		return FrozenRequest{}, errors.New("freeze request: incomplete frozen membership")
	}

	return frozen, nil
}

func bindFrozenRequest(ctx context.Context, tx dbx.Querier, ids []int64, baseID string) error {
	tag, err := tx.Exec(ctx, mustSQL("send_request_bind.sql"), ids, baseID)
	if err != nil {
		return fmt.Errorf("freeze request: bind: %w", err)
	}

	if tag.RowsAffected() != int64(len(ids)) {
		return errors.New("freeze request: bind fence changed")
	}

	return nil
}

type requestDeliveryState struct {
	ID            int64
	RoomID        string
	Status        string
	RowVersion    int64
	SendRequestID string
}

func validateRequestDeliveryStates(expected []domain.YouTubeNotificationDelivery, states []requestDeliveryState) (string, error) {
	if len(expected) != len(states) {
		return "", errors.New("freeze request: missing delivery")
	}

	byID := make(map[int64]domain.YouTubeNotificationDelivery, len(expected))
	for i := range expected {
		byID[expected[i].ID] = expected[i]
	}

	bound := states[0].SendRequestID
	for _, row := range states {
		before, ok := byID[row.ID]
		if !ok || row.Status != "PENDING" || row.RowVersion != before.RowVersion || row.RoomID != before.RoomID {
			return "", errors.New("freeze request: stale preparation fence")
		}

		if row.SendRequestID != bound {
			return "", errors.New("freeze request: mixed frozen membership")
		}
	}

	return bound, nil
}

func loadFrozenRequest(ctx context.Context, db dbx.Querier, id string) (FrozenRequest, error) {
	rows, err := db.Query(ctx, mustSQL("send_request_get.sql"), id)
	if err != nil {
		return FrozenRequest{}, fmt.Errorf("get frozen request: %w", err)
	}

	request, err := pgx.CollectExactlyOneRow(rows, scanFrozenRequest)
	if err != nil {
		return FrozenRequest{}, fmt.Errorf("get frozen request: scan: %w", err)
	}

	return request, nil
}

// AdvanceRequestGeneration은 확정 pre-handoff 실패의 새 세대와 정규 retry 전이를 원자 저장한다.
func (s *TransitionStore) AdvanceRequestGeneration(ctx context.Context, operation StartedOperation, request FrozenRequest) (FrozenRequest, error) {
	if !operation.Valid() || request.Generation >= iris.ReplyReissueMaxGenerations {
		return FrozenRequest{}, errors.New("advance request generation: invalid operation or exhausted generations")
	}

	transitions, _, at, _, err := s.prepareFailureTransitions("advance request generation", operation, lifecycle.FailureRetryable, "provider_transport", 0)
	if err != nil {
		return FrozenRequest{}, fmt.Errorf("advance request generation: failure transition: %w", err)
	}

	for i := range transitions {
		if transitions[i].after.Status == lifecycle.StatusFailed {
			return FrozenRequest{}, errors.New("advance request generation: retry budget exhausted")
		}
	}

	err = s.executeTx(ctx, "advance request generation", func(tx dbx.Querier) error {
		return advanceRequestGenerationInTx(ctx, tx, operation, request, transitions, at)
	})
	if err != nil {
		return FrozenRequest{}, fmt.Errorf("advance youtube request generation: %w", err)
	}

	request.Generation++

	return request, nil
}

func advanceRequestGenerationInTx(ctx context.Context, tx dbx.Querier, operation StartedOperation, request FrozenRequest, transitions []rowTransition, at time.Time) error {
	if err := verifyStartedRequestOwners(ctx, tx, operation, request.BaseID); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, mustSQL("send_request_advance.sql"), request.BaseID, request.Generation)
	if err != nil {
		return fmt.Errorf("advance request generation: update: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return errors.New("advance request generation: generation fence changed")
	}

	if _, err := applyRowTransitions(ctx, tx, "advance request generation", transitions); err != nil {
		return fmt.Errorf("advance request generation: retry transition: %w", err)
	}

	mode := DeliveryModePerRoom

	if operation.OwnerCount() > 1 {
		mode = DeliveryModeGrouped
	}

	if err := recordAttemptTelemetry(ctx, tx, mode, ownerAttempts(operation.groups, attemptResultFailure, "provider_transport"), at); err != nil {
		return fmt.Errorf("advance request generation: telemetry: %w", err)
	}

	return nil
}

func verifyStartedRequestOwners(ctx context.Context, tx dbx.Querier, operation StartedOperation, baseID string) error {
	groups := sortedStartedGroups(operation.groups)
	for i := range groups {
		group := &groups[i]

		var valid bool

		if err := tx.QueryRow(ctx, mustSQL("send_request_fence.sql"), group.ownerAfter.ID, group.ownerAfter.RowVersion, baseID).Scan(&valid); err != nil {
			return fmt.Errorf("advance request generation: owner fence: %w", err)
		}

		if !valid {
			return errors.New("advance request generation: stale owner")
		}
	}

	return nil
}

// CleanupOrphanRequests는 기존 terminal retention 뒤 참조가 사라진 본문만 제한된 batch로 삭제한다.
func (s *TransitionStore) CleanupOrphanRequests(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, errors.New("cleanup orphan requests: invalid limit")
	}

	tag, err := s.db.Exec(ctx, mustSQL("send_request_cleanup.sql"), cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("cleanup orphan requests: %w", err)
	}

	return tag.RowsAffected(), nil
}

func insertFrozenRequest(ctx context.Context, tx dbx.Querier, request FrozenRequest) (FrozenRequest, error) {
	sum := sha256.Sum256([]byte(request.Message))

	request.MessageHash = hex.EncodeToString(sum[:])

	if _, err := tx.Exec(ctx, mustSQL("send_request_insert.sql"), request.BaseID, request.RoomID, request.Message, request.MessageHash, request.DedupeKeys, request.MemberIDs, request.Route); err != nil {
		return FrozenRequest{}, fmt.Errorf("insert frozen request: %w", err)
	}

	frozen, err := loadFrozenRequest(ctx, tx, request.BaseID)
	if err != nil {
		return FrozenRequest{}, err
	}

	if frozen.RoomID != request.RoomID || frozen.Route != request.Route || frozen.MessageHash != request.MessageHash || !slices.Equal(frozen.MemberIDs, request.MemberIDs) || !slices.Equal(frozen.DedupeKeys, request.DedupeKeys) {
		return FrozenRequest{}, errors.New("insert frozen request: existing request collision")
	}

	return frozen, nil
}
