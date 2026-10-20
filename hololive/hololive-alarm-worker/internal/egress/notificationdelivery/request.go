package notificationdelivery

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// saveRequest는 claim 당시 시도 횟수의 발송 전 claim에 아직 고정되지 않은 요청을 고정한다. 조건이 맞지 않으면
// *sendfence.MissError다.
func (r *Store) saveRequest(ctx context.Context, id int64, workerID string, attemptCount int, request *deliverypayload.Request) error {
	requestJSON, err := jsonv2.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode delivery request: %w", err)
	}

	return r.applyFenced(ctx, actionSaveRequest, id, saveRequestSQL, id, workerID, attemptCount, string(requestJSON))
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

	if err := d.repository.saveRequest(ctx, item.ID, d.workerID, item.AttemptCount, request); err != nil {
		if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
			// 요청을 고정하기 전이라 외부 부수효과가 없으므로 소유권이 남았으면 일반 재시도 정책으로 실패 처리한다.
			d.settleRequestFenceMiss(ctx, item, miss, false, "delivery request snapshot fence miss")

			return nil, false
		}

		d.logger.Error("Failed to save delivery request", "id", item.ID, "error", err)

		return nil, false
	}

	return request, true
}

// sendPrepared는 고정 요청을 세대에 맞는 client request ID로 보내고, 성공이면 Iris requestId를 돌려준다.
func (d *Dispatcher) sendPrepared(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *deliverypayload.Request) (string, error) {
	requestID := request.BaseID
	if request.Generation > 0 {
		id, err := iris.ReissuedClientRequestID(request.BaseID, request.Generation)
		if err != nil {
			return "", fmt.Errorf("resolve delivery request ID: %w", err)
		}

		requestID = id
	}

	irisRequestID, err := d.sender.SendPreparedMessage(ctx, item.RoomID, request.Body, request.Route, requestID)
	if err != nil {
		return "", fmt.Errorf("send prepared delivery: %w", err)
	}

	return irisRequestID, nil
}

func (d *Dispatcher) reissueRequest(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *deliverypayload.Request, cause error) {
	exhaust := request.Generation >= iris.ReplyReissueMaxGenerations

	err := d.repository.reissueFailedRequest(ctx, item.ID, d.workerID, item.AttemptCount, request.Generation, exhaust, d.config.MaxRetries, d.config.RetryBackoff, cause.Error())
	if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
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

// reissueFailedRequest는 세대 증가(또는 소진 표시)와 다음 상태를 같은 commit에 저장해 crash 이후 저장된 새 ID로만
// 복구합니다. 요청 fence는 저장된 요청의 세대가 generation이고 아직 소진되지 않았음이며, 맞지 않으면
// *sendfence.MissError를 돌려줍니다.
func (r *Store) reissueFailedRequest(ctx context.Context, id int64, workerID string, attemptCount, generation int, exhaust bool, maxRetries int, backoff time.Duration, reason string) error {
	status, err := deliveryFailureStatus(attemptCount, maxRetries)
	if err != nil {
		return fmt.Errorf("reissue failed delivery policy: %w", err)
	}

	if exhaust {
		status = domain.DeliveryStatusFailed
		reason = "client request ID generations exhausted: " + reason
	}

	return r.applyFenced(ctx, actionReissueRequest, id, reissueFailedSQL,
		id, workerID, attemptCount, generation, exhaust, status, durationMilliseconds(backoff), reason, status == domain.DeliveryStatusPending,
	)
}

// settleRequestFenceMiss는 요청 고정 fence 미스를 지표와 로그로 남기고 처리한다. 소유권이 남은 미스는 소유자·시도
// 횟수 fence가 걸린 실패 전이로 바로 끝낸다. 요청을 소진으로 표시하는 경우(exhaustRequest)는 rearm되지 않는 종료
// 실패이고, 아니면 일반 재시도 정책을 따른다. 소유권을 잃었거나 실패 전이마저 fence에 막히면 새 소유자나 sweep이
// 만든 상태를 덮어쓰지 않도록 행을 건드리지 않는다.
func (d *Dispatcher) settleRequestFenceMiss(ctx context.Context, item *domain.NotificationDeliveryOutbox, miss *sendfence.MissError, exhaustRequest bool, reason string) {
	result := fenceMissResultFenceLost

	var transitionErr error

	// 이 원장의 전이는 단일 행이라 미스 행도 하나다.
	if cause := miss.Rows[0].Reason; cause.OwnershipHeld() {
		result, transitionErr = d.failFencedRequest(ctx, item, exhaustRequest, fmt.Sprintf("%s (%s)", reason, cause))
	}

	d.recordFenceMiss(item.ID, miss, result, transitionErr)
}

func (d *Dispatcher) failFencedRequest(ctx context.Context, item *domain.NotificationDeliveryOutbox, exhaustRequest bool, reason string) (string, error) {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer cancel()

	var err error

	if exhaustRequest {
		err = d.repository.markRequestExhausted(ctx, item.ID, d.workerID, item.AttemptCount, reason)
	} else {
		err = d.repository.MarkFailed(ctx, item.ID, d.workerID, item.AttemptCount, d.config.MaxRetries, d.config.RetryBackoff, reason)
	}

	if _, lost := errors.AsType[*sendfence.MissError](err); lost {
		return fenceMissResultFenceLost, nil
	}

	if err != nil {
		return fenceMissResultTransitionError, fmt.Errorf("fail fenced delivery request: %w", err)
	}

	return fenceMissResultFailed, nil
}

func (d *Dispatcher) markItemTerminalFailed(ctx context.Context, item *domain.NotificationDeliveryOutbox, reason string) {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer cancel()

	if err := d.repository.MarkFailed(ctx, item.ID, d.workerID, item.AttemptCount, 1, d.config.RetryBackoff, reason); err != nil {
		d.reportTransitionError(item.ID, err, "Failed to finish exhausted delivery request")
	}
}

func (d *Dispatcher) validateStoredRequest(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *deliverypayload.Request) (*deliverypayload.Request, bool) {
	if request.Exhausted {
		d.markItemTerminalFailed(ctx, item, "client request ID generations exhausted")

		return nil, false
	}

	if iris.ValidateClientRequestID(request.BaseID) != nil || request.Generation < 0 || request.Generation > iris.ReplyReissueMaxGenerations {
		if d.markItemSending(ctx, item) {
			d.markItemQuarantined(ctx, item, "stored delivery request identity invalid")
		}

		return nil, false
	}

	return request, true
}

// 준비 실패는 요청을 고정하기 전이라 외부 부수효과가 없으므로 일반 실패로 재시도한다.
func (d *Dispatcher) markPreparationFailed(ctx context.Context, item *domain.NotificationDeliveryOutbox, cause error) {
	d.markItemFailed(ctx, item, cause.Error())
}
