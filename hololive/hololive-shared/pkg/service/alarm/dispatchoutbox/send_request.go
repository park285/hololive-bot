package dispatchoutbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/iris-client-go/v3/iris"
)

// 발송 경로는 저장된 최종 요청의 일부이며 재시도 때 다시 판정하지 않는다.
const (
	SendRouteText     = "text"
	SendRouteMarkdown = "markdown"
)

// SendRequest는 하나의 send unit이 모든 세대에서 공유하는 최종 외부 요청이다.
type SendRequest struct {
	Body                string
	Route               string
	BodyHash            string
	RoomID              string
	DeliveryIDs         []int64
	BaseClientRequestID string
	ClientRequestID     string
	Generation          int
}

var (
	ErrSendRequestUnpinned = errors.New("alarm send request not yet pinned")
	ErrLegacySendRequest   = errors.New("alarm send request missing for previously attempted unit")
	ErrSendRequestFence    = errors.New("alarm send request ownership or membership changed")
)

// SendRequestRepository는 전체 send unit의 요청과 generation을 소유권 fence 아래 저장한다.
type SendRequestRepository interface {
	LoadSendRequest(context.Context, int64, []int64, string) (*SendRequest, error)
	PinSendRequest(context.Context, int64, []int64, string, SendRequest) (*SendRequest, error)
	ReissueSendRequest(context.Context, int64, []int64, string, string, []FailureUpdate) (bool, error)
}

// LoadSendRequest는 고정된 요청을 읽으며 확정 미발송·미고정이면 ErrSendRequestUnpinned를 반환한다.
func (r *PgxRepository) LoadSendRequest(ctx context.Context, unitID int64, ids []int64, worker string) (*SendRequest, error) {
	return r.prepareSendRequest(ctx, unitID, ids, worker, nil)
}

// PinSendRequest는 첫 발송 전에만 요청을 고정하고 이미 고정된 경우 저장된 요청을 반환한다.
func (r *PgxRepository) PinSendRequest(ctx context.Context, unitID int64, ids []int64, worker string, request SendRequest) (*SendRequest, error) {
	return r.prepareSendRequest(ctx, unitID, ids, worker, &request)
}

func (r *PgxRepository) prepareSendRequest(ctx context.Context, unitID int64, ids []int64, worker string, candidate *SendRequest) (*SendRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin send request: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			r.logger.Warn("alarm request rollback failed", slog.Any("error", rollbackErr))
		}
	}()

	request, err := lockSendRequest(ctx, tx, unitID)
	if err != nil {
		return nil, err
	}

	attempted, err := fenceSendRequest(ctx, tx, unitID, ids, worker, "leased")
	if err != nil {
		return nil, err
	}

	if request.BodyHash != "" {
		if !sameDeliveryIDs(request.DeliveryIDs, ids) {
			return nil, ErrSendRequestFence
		}

		return request, nil
	}

	if attempted {
		return nil, ErrLegacySendRequest
	}

	if candidate == nil {
		return nil, ErrSendRequestUnpinned
	}

	if candidate.Route != SendRouteText && candidate.Route != SendRouteMarkdown {
		return nil, errors.New("pin send request: unsupported route")
	}

	request.Body = candidate.Body
	request.Route = candidate.Route

	sum := sha256.Sum256([]byte(request.Body))

	request.BodyHash = hex.EncodeToString(sum[:])
	request.DeliveryIDs = slices.Sorted(slices.Values(ids))
	request.BaseClientRequestID = request.ClientRequestID

	_, err = tx.Exec(ctx, mustSQL("send_request_pin.sql"), unitID, request.Body, request.Route, request.BodyHash, request.DeliveryIDs, request.BaseClientRequestID)
	if err != nil {
		return nil, fmt.Errorf("pin send request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit send request: %w", err)
	}

	return request, nil
}

func lockSendRequest(ctx context.Context, tx pgx.Tx, unitID int64) (*SendRequest, error) {
	var request SendRequest

	err := tx.QueryRow(ctx, mustSQL("send_request_lock.sql"), unitID).Scan(&request.Body, &request.Route, &request.BodyHash, &request.DeliveryIDs, &request.BaseClientRequestID, &request.ClientRequestID, &request.Generation, &request.RoomID)
	if err != nil {
		return nil, fmt.Errorf("lock send request: %w", err)
	}

	return &request, nil
}

func fenceSendRequest(ctx context.Context, tx pgx.Tx, unitID int64, ids []int64, worker, status string) (bool, error) {
	rows, err := tx.Query(ctx, mustSQL("send_request_members.sql"), unitID)
	if err != nil {
		return false, fmt.Errorf("lock send request members: %w", err)
	}
	defer rows.Close()

	var actual []int64

	attempted := false
	valid := true

	for rows.Next() {
		var id int64

		var rowStatus, owner string

		var owned, started bool

		if err := rows.Scan(&id, &rowStatus, &owner, &owned, &started); err != nil {
			return false, fmt.Errorf("scan send request members: %w", err)
		}

		actual = append(actual, id)
		valid = valid && rowStatus == status && owner == worker && (status == "sending" || owned)
		attempted = attempted || started
	}

	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read send request members: %w", err)
	}

	if !valid || len(ids) == 0 || !sameDeliveryIDs(actual, ids) {
		return false, ErrSendRequestFence
	}

	return attempted, nil
}

func sameDeliveryIDs(left, right []int64) bool {
	return slices.Equal(slices.Sorted(slices.Values(left)), slices.Sorted(slices.Values(right)))
}

// ReissueSendRequest는 확정된 pre-handoff 실패를 새 세대와 retry 전이로 원자적으로 기록한다.
// 저장 뒤 종료돼도 다음 claim은 새 ID와 기존 본문을 그대로 복구한다.
func (r *PgxRepository) ReissueSendRequest(ctx context.Context, unitID int64, ids []int64, worker, expectedID string, updates []FailureUpdate) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin reissue request: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			r.logger.Warn("alarm request rollback failed", slog.Any("error", rollbackErr))
		}
	}()

	request, err := lockSendRequest(ctx, tx, unitID)
	if err != nil {
		return false, err
	}

	if _, fenceErr := fenceSendRequest(ctx, tx, unitID, ids, worker, "sending"); fenceErr != nil {
		return false, fenceErr
	}

	if request.BodyHash == "" || request.ClientRequestID != expectedID || !sameDeliveryIDs(request.DeliveryIDs, ids) {
		return false, ErrSendRequestFence
	}

	if request.Generation >= iris.ReplyReissueMaxGenerations {
		return false, nil
	}

	nextID, err := iris.ReissuedClientRequestID(request.BaseClientRequestID, request.Generation+1)
	if err != nil {
		return false, fmt.Errorf("derive reissue request: %w", err)
	}

	if requeueErr := requeueReissuedDeliveries(ctx, tx, ids, worker, updates); requeueErr != nil {
		return false, requeueErr
	}

	_, err = tx.Exec(ctx, mustSQL("send_request_reissue.sql"), unitID, nextID, request.Generation+1)
	if err != nil {
		return false, fmt.Errorf("persist reissued request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit reissued request: %w", err)
	}

	return true, nil
}

// BeginSendRequest는 고정된 전체 membership을 다시 검증한 뒤 단일 트랜잭션으로 발송 상태에 진입한다.
func (r *PgxRepository) BeginSendRequest(ctx context.Context, unitID int64, ids []int64, worker, expectedID string, lease time.Duration) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin prepared send: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			r.logger.Warn("alarm request rollback failed", slog.Any("error", rollbackErr))
		}
	}()

	request, err := lockSendRequest(ctx, tx, unitID)
	if err != nil {
		return err
	}

	if _, fenceErr := fenceSendRequest(ctx, tx, unitID, ids, worker, "leased"); fenceErr != nil {
		return fenceErr
	}

	if request.BodyHash == "" || request.ClientRequestID != expectedID || !sameDeliveryIDs(request.DeliveryIDs, ids) {
		return ErrSendRequestFence
	}

	tag, err := tx.Exec(ctx, mustSQL("repository_transitions_0020_01.sql"), ids, max(1, int(lease.Seconds())), worker)
	if err != nil {
		return fmt.Errorf("begin prepared delivery send: %w", err)
	}

	if tag.RowsAffected() != int64(len(ids)) {
		return ErrSendRequestFence
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit prepared send: %w", err)
	}

	return nil
}

func requeueReissuedDeliveries(ctx context.Context, tx pgx.Tx, ids []int64, worker string, updates []FailureUpdate) error {
	if len(updates) != len(ids) {
		return ErrSendRequestFence
	}

	for _, update := range updates {
		if update.TargetStatus != StatusRetry || !slices.Contains(ids, update.ID) {
			return ErrSendRequestFence
		}

		tag, updateErr := tx.Exec(ctx, mustSQL("send_request_reissue_delivery.sql"), update.ID, worker, update.AttemptCount, update.NextAttemptAt, sanitizeStoredError(update.Error), update.ErrorCode)
		if updateErr != nil {
			return fmt.Errorf("requeue reissued delivery: %w", updateErr)
		}

		if tag.RowsAffected() != 1 {
			return ErrSendRequestFence
		}
	}

	return nil
}
