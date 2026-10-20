package dispatchoutbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
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

// ErrSendRequestUnpinned는 fence는 맞지만 아직 요청을 고정하지 않았다는 정상 분기다.
var ErrSendRequestUnpinned = errors.New("alarm send request not yet pinned")

// SendRequestRepository는 전체 send unit의 요청과 generation을 소유권 fence 아래 저장한다. 미스는
// *sendfence.MissError이며, 재발급 세대를 모두 썼으면 sendfence.ErrGenerationExhausted다.
type SendRequestRepository interface {
	LoadSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string) (*SendRequest, error)
	PinSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string, request SendRequest) (*SendRequest, error)
	ReissueSendRequest(ctx context.Context, unitID int64, workerID string, request *SendRequest, updates []FailureUpdate) error
}

// LoadSendRequest는 고정된 요청을 읽으며 fence는 맞지만 미고정이면 ErrSendRequestUnpinned를 반환한다.
func (r *PgxRepository) LoadSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string) (*SendRequest, error) {
	return r.prepareSendRequest(ctx, unitID, claims, workerID, expectedID, nil)
}

// PinSendRequest는 첫 발송 전에만 요청을 고정하고 이미 고정된 경우 저장된 요청을 반환한다.
func (r *PgxRepository) PinSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string, request SendRequest) (*SendRequest, error) {
	return r.prepareSendRequest(ctx, unitID, claims, workerID, expectedID, &request)
}

// prepareSendRequest는 단위를 잠그고 단위 멤버 전원의 fence(leased·소유자·attempt·lease·멤버십·멤버 종단·요청 ID)를
// 확인한 뒤 고정 요청을 읽거나 candidate로 고정한다.
func (r *PgxRepository) prepareSendRequest(
	ctx context.Context,
	unitID int64,
	claims []DeliveryClaim,
	workerID, expectedID string,
	candidate *SendRequest,
) (*SendRequest, error) {
	action := fenceActionLoadSendRequest

	if candidate != nil {
		action = fenceActionPinSendRequest
	}

	if len(claims) == 0 {
		return nil, fmt.Errorf("%s: no deliveries", action)
	}

	raw, err := jsonv2.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal claims: %w", action, err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin send request: %w", err)
	}

	defer func() {
		if rollbackErr := pgxdb.Rollback(ctx, tx); rollbackErr != nil {
			r.logger.Warn("alarm request rollback failed", slog.Any("error", rollbackErr))
		}
	}()

	request, err := lockSendRequest(ctx, tx, unitID)
	if err != nil {
		return nil, err
	}

	ids := fencedTargetIDs(claims)
	if err := r.applyFenced(ctx, tx, action, sqlSendRequestMembers, ids, unitID, jsonbRecordsetParam(raw), workerID, expectedID); err != nil {
		return nil, err
	}

	if request.BodyHash != "" {
		return request, nil
	}

	if candidate == nil {
		return nil, ErrSendRequestUnpinned
	}

	if err := fillPinnedRequest(request, candidate, ids); err != nil {
		return nil, err
	}

	if err := pinSendRequest(ctx, tx, unitID, request, claims); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit send request: %w", err)
	}

	return request, nil
}

// fillPinnedRequest는 잠근 미고정 요청에 candidate 본문·경로와 멤버 ID, 첫 세대 요청 ID를 채운다.
func fillPinnedRequest(request, candidate *SendRequest, ids []int64) error {
	if candidate.Route != SendRouteText && candidate.Route != SendRouteMarkdown {
		return errors.New("pin send request: unsupported route")
	}

	request.Body = candidate.Body
	request.Route = candidate.Route

	sum := sha256.Sum256([]byte(request.Body))

	request.BodyHash = hex.EncodeToString(sum[:])
	request.DeliveryIDs = slices.Sorted(slices.Values(ids))
	request.BaseClientRequestID = request.ClientRequestID

	return nil
}

func lockSendRequest(ctx context.Context, tx pgx.Tx, unitID int64) (*SendRequest, error) {
	var request SendRequest

	err := tx.QueryRow(ctx, mustSQL("send_request_lock.sql"), unitID).Scan(&request.Body, &request.Route, &request.BodyHash, &request.DeliveryIDs, &request.BaseClientRequestID, &request.ClientRequestID, &request.Generation, &request.RoomID)
	if err != nil {
		return nil, fmt.Errorf("lock send request: %w", err)
	}

	return &request, nil
}

// pinSendRequest는 잠근 단위의 요청이 아직 비어 있을 때만 고정한다. 고정하지 못하면 다른 고정이 앞선 것이므로
// 단위 전원을 request_changed 미스로 보고한다.
func pinSendRequest(ctx context.Context, tx pgx.Tx, unitID int64, request *SendRequest, claims []DeliveryClaim) error {
	rows, err := tx.Query(ctx, mustSQL("send_request_pin.sql"), unitID, request.Body, request.Route, request.BodyHash, request.DeliveryIDs, request.BaseClientRequestID)
	if err != nil {
		return fmt.Errorf("pin send request: %w", err)
	}

	pinned, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return fmt.Errorf("pin send request: %w", err)
	}

	if len(pinned) == 1 {
		return nil
	}

	observed := make([]sendfence.Observation, 0, len(claims))
	for _, claim := range claims {
		observed = append(observed, sendfence.Observation{
			ID: claim.ID, Status: string(StatusLeased),
			StatusMatches: true, OwnerMatches: true, VersionMatches: true, LeaseValid: true,
			MembershipMatches: true, MembersNonTerminal: true,
		})
	}

	return fmt.Errorf("pin send request: %w", sendfence.Check(fenceActionPinSendRequest, fencedTargetIDs(claims), observed))
}

// ReissueSendRequest는 확정된 pre-handoff 실패를 새 세대와 멤버 전원의 retry 전이로 한 문장에 기록한다. 인자 request는
// 이 claim에서 fence 아래 읽은 고정 요청이며, SQL은 그 client_request_id·세대가 그대로일 때만 바꾼다. 저장 뒤
// 종료돼도 다음 claim은 새 ID와 기존 본문을 그대로 복구한다.
func (r *PgxRepository) ReissueSendRequest(ctx context.Context, unitID int64, workerID string, request *SendRequest, updates []FailureUpdate) error {
	if request.Generation >= iris.ReplyReissueMaxGenerations {
		return sendfence.ErrGenerationExhausted
	}

	if len(updates) == 0 {
		return errors.New("reissue send request: no deliveries")
	}

	if err := validateFailureUpdates(updates, fenceActionReissueSendRequest); err != nil {
		return err
	}

	for i := range updates {
		if updates[i].TargetStatus != StatusRetry {
			return fmt.Errorf("%s: unsupported target status %q for delivery %d", fenceActionReissueSendRequest, updates[i].TargetStatus, updates[i].ID)
		}
	}

	nextID, err := iris.ReissuedClientRequestID(request.BaseClientRequestID, request.Generation+1)
	if err != nil {
		return fmt.Errorf("derive reissue request: %w", err)
	}

	raw, err := jsonv2.Marshal(prepareFailureUpdates(updates))
	if err != nil {
		return fmt.Errorf("%s: marshal batch: %w", fenceActionReissueSendRequest, err)
	}

	return r.applyFenced(ctx, r.pool, fenceActionReissueSendRequest, sqlReissueSendRequest, fencedTargetIDs(updates),
		unitID, jsonbRecordsetParam(raw), workerID, request.ClientRequestID, request.Generation, nextID)
}

// BeginSendRequest는 고정된 전체 membership·요청 ID·소유권·lease를 한 문장에서 다시 확인하고 단위 전원을 sending으로
// 바꾼다. 하나라도 맞지 않으면 아무 행도 바꾸지 않고 *sendfence.MissError를 돌려준다.
func (r *PgxRepository) BeginSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string, lease time.Duration) error {
	leaseMS, err := leaseMilliseconds(lease)
	if err != nil {
		return fmt.Errorf("%s: lease: %w", fenceActionBeginSendRequest, err)
	}

	if len(claims) == 0 {
		return errors.New("begin send request: no deliveries")
	}

	raw, err := jsonv2.Marshal(claims)
	if err != nil {
		return fmt.Errorf("%s: marshal claims: %w", fenceActionBeginSendRequest, err)
	}

	return r.applyFenced(ctx, r.pool, fenceActionBeginSendRequest, sqlBeginSendRequest, fencedTargetIDs(claims),
		unitID, jsonbRecordsetParam(raw), workerID, expectedID, leaseMS)
}
