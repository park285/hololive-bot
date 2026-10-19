package notificationdelivery

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// requestFenceOperation은 요청 고정 갱신의 종류이며 fence 미스 지표의 operation label이다.
type requestFenceOperation string

const (
	requestFenceSave    requestFenceOperation = "save_request"
	requestFenceReissue requestFenceOperation = "reissue_request"
)

// requestFenceCause는 요청 고정 갱신이 0행일 때 잠근 행에서 관측한 첫 불일치다. 판정은 선언 순서를 따르며,
// requestFenceRequestChanged만 상태·소유자·lease·시도 횟수가 모두 맞아 이 worker가 아직 행을 소유하는 미스다.
type requestFenceCause string

const (
	// 행이 없다. 다른 경로가 종료 상태로 끝낸 뒤 보존 정리가 지운 경우다.
	requestFenceRowMissing requestFenceCause = "row_missing"
	// 갱신이 허용하는 상태가 아니다. 다른 소유자의 정산이나 stale SENDING sweep 격리가 앞선 경우다.
	requestFenceStatusChanged requestFenceCause = "status_changed"
	// 이 worker가 소유자가 아니다. 다른 worker가 lease 만료 뒤 재claim했거나 다른 전이가 소유자를 비운 경우다.
	requestFenceOwnerChanged requestFenceCause = "owner_changed"
	// 발송 전 PENDING claim의 lease가 만료됐다. 아직 재claim되지 않았어도 다음 claim이 가져갈 수 있다.
	requestFenceLeaseExpired requestFenceCause = "lease_expired"
	// 시도 횟수(attempt_count)가 claim 당시와 다르다. 같은 소유자 이름으로 다른 claim 세대가 정산·재claim된 경우다.
	requestFenceAttemptChanged requestFenceCause = "attempt_changed"
	// 저장된 요청이 기대한 이전 요청과 다르다. 다른 writer가 세대·본문을 바꿨거나, 같은 요청이 Go 재인코딩과 다른
	// JSON 표현으로 저장된 경우다.
	requestFenceRequestChanged requestFenceCause = "request_changed"
)

// requestFenceMissError는 요청 고정 갱신이 fence 불일치로 아무 행도 바꾸지 않았다는 뜻이다. 원인은 갱신과 같은
// 문장에서 잡은 행 잠금 아래에서 관측한다.
type requestFenceMissError struct {
	Operation requestFenceOperation
	Cause     requestFenceCause
	// Status는 관측한 행 상태이며 행이 없으면 빈 값이다.
	Status domain.DeliveryOutboxStatus
}

func (e *requestFenceMissError) Error() string {
	return fmt.Sprintf("%s fence miss: %s (observed status %q)", e.Operation, e.Cause, e.Status)
}

// ownershipHeld는 미스 뒤에도 이 worker가 행을 소유하는지 보고한다. 소유권이 남은 미스만 실패 전이 대상이다.
func (e *requestFenceMissError) ownershipHeld() bool {
	return e.Cause == requestFenceRequestChanged
}

// execRequestFence는 요청 고정 SQL을 실행한다. SQL은 돌려주는 fence 조건이 모두 참일 때만 갱신하므로, 조건이 모두
// 참이면 nil이고 하나라도 거짓이면 첫 원인을 담은 *requestFenceMissError다.
func (r *Store) execRequestFence(ctx context.Context, operation requestFenceOperation, query string, args ...any) error {
	var (
		status                                           string
		statusOK, ownerOK, leaseOK, attemptOK, requestOK bool
	)

	err := r.pool.QueryRow(ctx, query, args...).Scan(&status, &statusOK, &ownerOK, &leaseOK, &attemptOK, &requestOK)
	if errors.Is(err, pgx.ErrNoRows) {
		return &requestFenceMissError{Operation: operation, Cause: requestFenceRowMissing}
	}

	if err != nil {
		return fmt.Errorf("query request fence: %w", err)
	}

	var cause requestFenceCause

	switch {
	case !statusOK:
		cause = requestFenceStatusChanged
	case !ownerOK:
		cause = requestFenceOwnerChanged
	case !leaseOK:
		cause = requestFenceLeaseExpired
	case !attemptOK:
		cause = requestFenceAttemptChanged
	case !requestOK:
		cause = requestFenceRequestChanged
	default:
		return nil
	}

	return &requestFenceMissError{Operation: operation, Cause: cause, Status: domain.DeliveryOutboxStatus(status)}
}

// saveRequest는 fence가 맞지 않으면 *requestFenceMissError를 돌려준다.
func (r *Store) saveRequest(ctx context.Context, id int64, workerID string, previous, next *deliverypayload.Request) error {
	if err := r.ensurePool(); err != nil {
		return fmt.Errorf("save delivery request: %w", err)
	}

	previousJSON, err := jsonv2.Marshal(previous)
	if err != nil {
		return fmt.Errorf("encode previous delivery request: %w", err)
	}

	nextJSON, err := jsonv2.Marshal(next)
	if err != nil {
		return fmt.Errorf("encode delivery request: %w", err)
	}

	if err := r.execRequestFence(ctx, requestFenceSave, saveRequestSQL, id, workerID, string(previousJSON), string(nextJSON)); err != nil {
		return fmt.Errorf("save delivery request: %w", err)
	}

	return nil
}

func (d *Dispatcher) prepareRequest(ctx context.Context, item *domain.NotificationDeliveryOutbox, payload deliverypayload.Payload) (*deliverypayload.Request, bool) {
	if payload.Request != nil {
		return d.validateStoredRequest(ctx, item, payload.Request)
	}

	body, route, err := d.sender.PrepareMessageRequest(ctx, item.RoomID, payload.Message)
	if err != nil {
		d.markPreparationFailed(ctx, item, err)

		return nil, false
	}

	request := &deliverypayload.Request{Body: body, Route: route, BaseID: notificationDeliveryClientRequestID(item)}

	if err := d.repository.saveRequest(ctx, item.ID, d.workerID, nil, request); err != nil {
		if miss, ok := errors.AsType[*requestFenceMissError](err); ok {
			// 요청을 고정하기 전이라 외부 부수효과가 없으므로 소유권이 남았으면 일반 재시도 정책으로 실패 처리한다.
			d.settleRequestFenceMiss(ctx, item, miss, false, "delivery request snapshot fence miss")

			return nil, false
		}

		d.logger.Error("Failed to save delivery request", "id", item.ID, "error", err)

		return nil, false
	}

	return request, true
}

func (d *Dispatcher) sendPrepared(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *deliverypayload.Request) error {
	requestID := request.BaseID
	if request.Generation > 0 {
		id, err := iris.ReissuedClientRequestID(request.BaseID, request.Generation)
		if err != nil {
			return fmt.Errorf("resolve delivery request ID: %w", err)
		}

		requestID = id
	}

	if err := d.sender.SendPreparedMessage(ctx, item.RoomID, request.Body, request.Route, requestID); err != nil {
		return fmt.Errorf("send prepared delivery: %w", err)
	}

	return nil
}

func (d *Dispatcher) reissueRequest(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *deliverypayload.Request, cause error) {
	next := *request
	if request.Generation >= iris.ReplyReissueMaxGenerations {
		next.Exhausted = true
	} else {
		next.Generation++
	}

	err := d.repository.reissueFailedRequest(ctx, item.ID, d.workerID, item.AttemptCount, request, &next, d.config.MaxRetries, d.config.RetryBackoff, cause.Error())
	if miss, ok := errors.AsType[*requestFenceMissError](err); ok {
		// 409 CLIENT_REQUEST_ID_FAILED는 확정된 pre-handoff 실패지만 새 세대를 저장하지 못했다. 같은 ID로 다시 보내면
		// 같은 충돌을 반복하므로 소유권이 남았으면 저장된 요청을 소진으로 표시하고 종료 실패로 끝낸다.
		d.settleRequestFenceMiss(ctx, item, miss, true, "client request ID reissue fence miss: "+cause.Error())

		return
	}

	// 그 밖의 오류는 갱신 commit 여부를 알 수 없으므로 SENDING으로 남겨 stale sweep이 격리한다.
	if err != nil {
		d.logger.Error("Failed to save delivery request generation and retry", "id", item.ID, "error", err)
	}
}

// 세대 증가와 다음 상태를 같은 commit에 저장해 crash 이후 저장된 새 ID로만 복구합니다. 갱신 fence가 맞지 않으면
// *requestFenceMissError를 돌려줍니다.
func (r *Store) reissueFailedRequest(ctx context.Context, id int64, workerID string, attemptCount int, previous, next *deliverypayload.Request, maxRetries int, backoff time.Duration, reason string) error {
	if err := r.ensurePool(); err != nil {
		return fmt.Errorf("reissue failed delivery: %w", err)
	}

	previousJSON, err := jsonv2.Marshal(previous)
	if err != nil {
		return fmt.Errorf("encode previous delivery request: %w", err)
	}

	nextJSON, err := jsonv2.Marshal(next)
	if err != nil {
		return fmt.Errorf("encode reissued delivery request: %w", err)
	}

	status, err := deliveryFailureStatus(attemptCount, maxRetries)
	if err != nil {
		return fmt.Errorf("reissue failed delivery policy: %w", err)
	}

	if next.Exhausted {
		status = domain.DeliveryStatusFailed
		reason = "client request ID generations exhausted: " + reason
	}

	if err := r.execRequestFence(ctx, requestFenceReissue, reissueFailedSQL, id, workerID, string(previousJSON), string(nextJSON), status, durationMilliseconds(backoff), reason, status == domain.DeliveryStatusPending, attemptCount); err != nil {
		return fmt.Errorf("reissue failed delivery: %w", err)
	}

	return nil
}

// settleRequestFenceMiss는 요청 고정 fence 미스를 지표와 로그로 남기고 처리한다. 소유권이 남은 미스는 소유자·시도
// 횟수 fence가 걸린 실패 전이로 바로 끝낸다. 요청을 소진으로 표시하는 경우(exhaustRequest)는 rearm되지 않는 종료
// 실패이고, 아니면 일반 재시도 정책을 따른다. 소유권을 잃었거나 실패 전이마저 fence에 막히면 새 소유자나 sweep이
// 만든 상태를 덮어쓰지 않도록 행을 건드리지 않는다.
func (d *Dispatcher) settleRequestFenceMiss(ctx context.Context, item *domain.NotificationDeliveryOutbox, miss *requestFenceMissError, exhaustRequest bool, reason string) {
	result := requestFenceResultFenceLost

	var transitionErr error

	if miss.ownershipHeld() {
		result, transitionErr = d.failFencedRequest(ctx, item, exhaustRequest, fmt.Sprintf("%s (%s)", reason, miss.Cause))
	}

	notificationDeliveryRequestFenceMissTotal.WithLabelValues(string(miss.Operation), string(miss.Cause), result).Inc()

	attrs := []any{
		slog.Int64("id", item.ID), slog.String("operation", string(miss.Operation)), slog.String("cause", string(miss.Cause)),
		slog.String("observed_status", string(miss.Status)), slog.String("result", result),
	}

	switch result {
	case requestFenceResultFailed:
		d.logger.Warn("Delivery request fence miss settled as failure", attrs...)
	case requestFenceResultFenceLost:
		d.logger.Warn("Delivery request fence lost; row left to its current owner", attrs...)
	default:
		d.logger.Error("Failed to settle delivery request fence miss", append(attrs, slog.Any("error", transitionErr))...)
	}
}

func (d *Dispatcher) failFencedRequest(ctx context.Context, item *domain.NotificationDeliveryOutbox, exhaustRequest bool, reason string) (string, error) {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer cancel()

	var (
		fenced bool
		err    error
	)

	if exhaustRequest {
		fenced, err = d.repository.markRequestExhausted(ctx, item.ID, d.workerID, item.AttemptCount, reason)
	} else {
		fenced, err = d.repository.MarkFailed(ctx, item.ID, d.workerID, item.AttemptCount, d.config.MaxRetries, d.config.RetryBackoff, reason)
	}

	if err != nil {
		return requestFenceResultTransitionError, fmt.Errorf("fail fenced delivery request: %w", err)
	}

	if !fenced {
		return requestFenceResultFenceLost, nil
	}

	return requestFenceResultFailed, nil
}

func (d *Dispatcher) markItemTerminalFailed(ctx context.Context, item *domain.NotificationDeliveryOutbox, reason string) {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer cancel()

	if _, err := d.repository.MarkFailed(ctx, item.ID, d.workerID, item.AttemptCount, 1, d.config.RetryBackoff, reason); err != nil {
		d.logger.Error("Failed to finish exhausted delivery request", "id", item.ID, "error", err)
	}
}

func (d *Dispatcher) validateStoredRequest(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *deliverypayload.Request) (*deliverypayload.Request, bool) {
	if request.Exhausted {
		d.markItemTerminalFailed(ctx, item, "client request ID generations exhausted")

		return nil, false
	}

	if iris.ValidateClientRequestID(request.BaseID) != nil || request.Generation < 0 || request.Generation > iris.ReplyReissueMaxGenerations {
		if d.markItemSending(ctx, item.ID) {
			d.markItemQuarantined(ctx, item.ID, "stored delivery request identity invalid")
		}

		return nil, false
	}

	return request, true
}

// 준비 실패는 요청을 고정하기 전이라 외부 부수효과가 없으므로 일반 실패로 재시도한다.
func (d *Dispatcher) markPreparationFailed(ctx context.Context, item *domain.NotificationDeliveryOutbox, cause error) {
	d.markItemFailed(ctx, item, cause.Error())
}
