// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func partitionGroupedDeliveries(
	group *deliveryGroup,
) ([]domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationOutbox, []domain.YouTubeNotificationDelivery) {
	validRows := make([]domain.YouTubeNotificationDelivery, 0, len(group.rows))
	validOutboxes := make([]domain.YouTubeNotificationOutbox, 0, len(group.outboxes))
	invalidRows := make([]domain.YouTubeNotificationDelivery, 0)

	for i := range group.outboxes {
		if validateOutboxPayload(&group.outboxes[i]) {
			validOutboxes = append(validOutboxes, group.outboxes[i])
			validRows = append(validRows, group.rows[i])

			continue
		}

		invalidRows = append(invalidRows, group.rows[i])
	}

	return validRows, validOutboxes, invalidRows
}

func (d *SendEngine) dispatchRowsIndividually(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *claimDecisionCache,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	for i := range rows {
		d.dispatchDeliveryRow(ctx, &rows[i], outboxByID, formattedMessages, formatFailures, reuseCache, result, mu)
	}
}

func (d *SendEngine) formatGroupedMessage(
	ctx context.Context,
	group *deliveryGroup,
	validOutboxes []domain.YouTubeNotificationOutbox,
) (string, error) {
	if group == nil {
		return "", errors.New("format grouped message: delivery group is missing")
	}

	memberName, err := d.formatter.GetMemberName(ctx, group.channelID)
	if err != nil {
		return "", fmt.Errorf("format grouped delivery member name: %w", fmt.Errorf("get member name: %w", err))
	}

	if memberName == "" {
		memberName = d.formatter.MessageStrings.Text(messagestrings.MiscVTuberFallback)
	}

	message, err := d.formatter.FormatGroupedMessage(ctx, memberName, group.channelID, group.kind, validOutboxes)
	if err != nil {
		return "", fmt.Errorf("format grouped delivery message: %w", fmt.Errorf("format grouped message: %w", err))
	}

	return message, nil
}

func singleDeliveryBatch(
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
) ([]domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationOutbox) {
	return []domain.YouTubeNotificationDelivery{*row}, []domain.YouTubeNotificationOutbox{*outbox}
}

// preFormatMessages: outbox_id별로 메시지를 1회 포맷하여 캐싱.
func (d *SendEngine) preFormatMessages(ctx context.Context, outboxByID map[int64]domain.YouTubeNotificationOutbox) (messages map[int64]string, failures map[int64]bool) {
	messages = make(map[int64]string, len(outboxByID))
	failures = make(map[int64]bool)

	for id := range outboxByID {
		item := outboxByID[id]

		msg, err := d.formatter.FormatMessage(ctx, &item)
		if err != nil {
			d.logger.Warn("Failed to pre-format outbox message",
				slog.Int64("outbox_id", id),
				slog.Any("error", fmt.Errorf("format message: %w", err)))

			failures[id] = true

			continue
		}

		messages[id] = msg
	}

	return
}

func (d *SendEngine) sendDeliveryMessage(ctx context.Context, req deliverySendRequest) (sendErr error) {
	if err := validateDeliverySendRequest(req); err != nil {
		return fmt.Errorf("validate delivery send request: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("send delivery message before request: %w", err)
	}

	sendCtx := ctx
	cancel := func() {}

	if d.config.DeliverySendTimeout > 0 {
		sendCtx, cancel = context.WithTimeoutCause(ctx, d.config.DeliverySendTimeout, errDeliverySendTimeout)
	}

	defer cancel()

	prepared, preparedOK := d.sender.(messagedelivery.PreparedMessageSender)
	usePrepared := req.frozen != nil && req.frozen.Route != "sender"

	if usePrepared && !preparedOK {
		return errors.New("send frozen delivery: prepared sender required")
	}

	finish := d.beginProviderAttempt()
	completed := false

	defer func() { finish(sendErr, completed) }()

	var err error

	if usePrepared {
		err = prepared.SendPreparedMessage(sendCtx, req.roomID, req.message, req.frozen.Route, req.requestID())
	} else if sender, ok := d.sender.(messagedelivery.ClientRequestMessageSender); ok {
		err = sender.SendMessageWithClientRequestID(sendCtx, req.roomID, req.message, req.requestID())
	} else {
		err = d.sender.SendMessage(sendCtx, req.roomID, req.message)
	}

	completed = true

	if err != nil {
		return d.wrapDeliverySendError(sendCtx, err)
	}

	return nil
}

func (d *SendEngine) wrapDeliverySendError(sendCtx context.Context, err error) error {
	if errors.Is(context.Cause(sendCtx), errDeliverySendTimeout) {
		return fmt.Errorf("send delivery message timed out after %s: %w", d.config.DeliverySendTimeout, errors.Join(errDeliverySendOutcomeUnknown, errDeliverySendTimeout, err))
	}

	if deliverySendOutcomeUnknown(err) {
		return fmt.Errorf("send delivery message: %w", errors.Join(errDeliverySendOutcomeUnknown, err))
	}

	return fmt.Errorf("send delivery message: %w", err)
}

// 결과 미확정(outcome unknown) 송신은 claim 해제도, FailureBuckets/Success 기록도 하지 않는다.
// 행이 SENDING+locked 상태로 남아야 QuarantineStaleSending(LockTimeout)이 격리하며,
// 여기서 실패로 처리하면 이미 Iris에 도달했을 수 있는 메시지가 재전송되어 중복 게시된다.
func (d *SendEngine) recordPerRoomSendOutcomeUnknown(
	row *domain.YouTubeNotificationDelivery,
	sendReq deliverySendRequest,
	sendErr error,
) {
	d.logger.Warn("Per-room delivery send outcome unknown, holding row for stale sending quarantine",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
}

func (d *SendEngine) recordGroupedSendOutcomeUnknown(
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	sendReq deliverySendRequest,
	sendErr error,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	d.logger.Warn("Grouped delivery send outcome unknown, holding rows for stale sending quarantine",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validRows)),
		slog.Any("delivery_ids", collectDeliveryIDs(validRows)),
		slog.Any("outbox_ids", collectDeliveryOutboxIDs(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
}

func (d *SendEngine) deliveryParallelism() int {
	if d.config.DeliveryParallelism > 0 {
		return d.config.DeliveryParallelism
	}

	return dispatchstate.DefaultConfig().DeliveryParallelism
}

func (r deliverySendRequest) requestID() string {
	if r.clientRequestID != "" {
		return r.clientRequestID
	}

	return deliveryClientRequestID(r.roomID, r.dedupeKeys)
}
