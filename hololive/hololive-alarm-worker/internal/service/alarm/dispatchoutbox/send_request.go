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
	"github.com/park285/iris-client-go/v6/iris"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

const (
	SendRouteText     = "text"
	SendRouteRichText = "richtext"
)

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

var ErrSendRequestUnpinned = errors.New("alarm send request not yet pinned")

type SendRequestRepository interface {
	LoadSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string) (*SendRequest, error)
	PinSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string, request SendRequest) (*SendRequest, error)
	ReissueSendRequest(ctx context.Context, unitID int64, workerID string, request *SendRequest, updates []FailureUpdate) error
}

func (r *PgxRepository) LoadSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string) (*SendRequest, error) {
	return r.prepareSendRequest(ctx, unitID, claims, workerID, expectedID, nil)
}

func (r *PgxRepository) PinSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string, request SendRequest) (*SendRequest, error) {
	return r.prepareSendRequest(ctx, unitID, claims, workerID, expectedID, &request)
}

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
	if err := r.applyFenced(ctx, tx, action, sendRequestMembersSQL, ids, unitID, jsonbRecordsetParam(raw), workerID, expectedID); err != nil {
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

func fillPinnedRequest(request, candidate *SendRequest, ids []int64) error {
	if candidate.Route != SendRouteText && candidate.Route != SendRouteRichText {
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

	err := pgxdb.QueryRow(ctx, tx, sendRequestLockSQL, unitID).Scan(&request.Body, &request.Route, &request.BodyHash, &request.DeliveryIDs, &request.BaseClientRequestID, &request.ClientRequestID, &request.Generation, &request.RoomID)
	if err != nil {
		return nil, fmt.Errorf("lock send request: %w", err)
	}

	return &request, nil
}

func pinSendRequest(ctx context.Context, tx pgx.Tx, unitID int64, request *SendRequest, claims []DeliveryClaim) error {
	rows, err := pgxdb.Query(ctx, tx, sendRequestPinSQL, unitID, request.Body, request.Route, request.BodyHash, request.DeliveryIDs, request.BaseClientRequestID)
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

	return r.applyFenced(ctx, r.pool, fenceActionReissueSendRequest, sendRequestReissueSQL, fencedTargetIDs(updates),
		unitID, jsonbRecordsetParam(raw), workerID, request.ClientRequestID, request.Generation, nextID)
}

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

	return r.applyFenced(ctx, r.pool, fenceActionBeginSendRequest, sendRequestBeginSQL, fencedTargetIDs(claims),
		unitID, jsonbRecordsetParam(raw), workerID, expectedID, leaseMS)
}
