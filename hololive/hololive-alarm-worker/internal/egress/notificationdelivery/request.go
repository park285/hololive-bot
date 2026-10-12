package notificationdelivery

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"time"

	"github.com/park285/iris-client-go/v3/iris"

	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Store) saveRequest(ctx context.Context, id int64, workerID string, previous, next *deliverypayload.Request) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, fmt.Errorf("save delivery request: %w", err)
	}

	previousJSON, err := jsonv2.Marshal(previous)
	if err != nil {
		return false, fmt.Errorf("encode previous delivery request: %w", err)
	}

	nextJSON, err := jsonv2.Marshal(next)
	if err != nil {
		return false, fmt.Errorf("encode delivery request: %w", err)
	}

	tag, err := r.pool.Exec(ctx, mustSQL("outbox_save_request.sql"), id, workerID, string(previousJSON), string(nextJSON))
	if err != nil {
		return false, fmt.Errorf("save delivery request: %w", err)
	}

	return tag.RowsAffected() > 0, nil
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

	saved, err := d.repository.saveRequest(ctx, item.ID, d.workerID, nil, request)
	if err != nil {
		d.logger.Error("Failed to save delivery request", "id", item.ID, "error", err)

		return nil, false
	}

	return request, saved
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

	_, err := d.repository.reissueFailedRequest(ctx, item.ID, d.workerID, item.AttemptCount, request, &next, d.config.MaxRetries, d.config.RetryBackoff, cause.Error())
	if err != nil {
		d.logger.Error("Failed to save delivery request generation and retry", "id", item.ID, "error", err)
	}
}

// 세대 증가와 다음 상태를 같은 commit에 저장해 crash 이후 저장된 새 ID로만 복구합니다.
func (r *Store) reissueFailedRequest(ctx context.Context, id int64, workerID string, attemptCount int, previous, next *deliverypayload.Request, maxRetries int, backoff time.Duration, reason string) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, fmt.Errorf("reissue failed delivery: %w", err)
	}

	previousJSON, err := jsonv2.Marshal(previous)
	if err != nil {
		return false, fmt.Errorf("encode previous delivery request: %w", err)
	}

	nextJSON, err := jsonv2.Marshal(next)
	if err != nil {
		return false, fmt.Errorf("encode reissued delivery request: %w", err)
	}

	status, err := deliveryFailureStatus(attemptCount, maxRetries)
	if err != nil {
		return false, fmt.Errorf("reissue failed delivery policy: %w", err)
	}

	if next.Exhausted {
		status = domain.DeliveryStatusFailed
		reason = "client request ID generations exhausted: " + reason
	}

	tag, err := r.pool.Exec(ctx, reissueFailedSQL, id, workerID, string(previousJSON), string(nextJSON), status, durationMilliseconds(backoff), reason, status == domain.DeliveryStatusPending, attemptCount)
	if err != nil {
		return false, fmt.Errorf("reissue failed delivery: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

func (d *Dispatcher) markItemTerminalFailed(ctx context.Context, item *domain.NotificationDeliveryOutbox, reason string) {
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
