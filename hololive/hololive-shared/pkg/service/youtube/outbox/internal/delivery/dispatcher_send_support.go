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

package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache/claim"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
)

func partitionGroupedDeliveries(
	group deliveryGroup,
) ([]domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationOutbox, []domain.YouTubeNotificationDelivery) {
	var validRows []domain.YouTubeNotificationDelivery
	var validOutboxes []domain.YouTubeNotificationOutbox
	var invalidRows []domain.YouTubeNotificationDelivery

	for i := range group.outboxes {
		if validateOutboxPayload(group.outboxes[i]) {
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
	reuseCache claim.DecisionCache,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	for i := range rows {
		d.dispatchDeliveryRow(ctx, rows[i], outboxByID, formattedMessages, formatFailures, reuseCache, result, mu)
	}
}

func (d *SendEngine) formatGroupedMessage(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
) (string, bool) {
	memberName, err := d.formatter.getMemberName(ctx, group.channelID)
	if err != nil || memberName == "" {
		memberName = "VTuber"
	}

	message, err := d.formatter.formatGroupedMessage(ctx, memberName, group.channelID, group.kind, validOutboxes)
	if err != nil {
		d.logger.Warn("Grouped format failed, falling back to individual dispatch",
			slog.String("room_id", group.roomID),
			slog.String("channel_id", group.channelID),
			slog.String("kind", string(group.kind)),
			slog.Int("count", len(validRows)),
			slog.Any("error", err))
		return "", false
	}

	return message, true
}

func (d *SendEngine) dispatchClaimedRowsIndividually(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	rowClaimTokens [][]dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	for i := range rows {
		var claims []dispatchstate.ClaimToken
		if i < len(rowClaimTokens) {
			claims = rowClaimTokens[i]
		}
		d.dispatchClaimedDeliveryRow(ctx, rows[i], outboxes[i], formattedMessages, formatFailures, claims, result, mu)
	}
}

func singleDeliveryBatch(
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
) ([]domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationOutbox) {
	return []domain.YouTubeNotificationDelivery{row}, []domain.YouTubeNotificationOutbox{outbox}
}

func (d *ClaimManager) releaseDeliveryClaimsWithWarning(
	ctx context.Context,
	claims []dispatchstate.ClaimToken,
	message string,
	attrs ...any,
) {
	if releaseErr := d.releaseDeliveryClaims(ctx, claims); releaseErr != nil && d.logger != nil {
		d.logger.Warn(message, append(attrs, slog.Any("error", releaseErr))...)
	}
}

// preFormatMessages: outbox_id별로 메시지를 1회 포맷하여 캐싱
func (d *SendEngine) preFormatMessages(ctx context.Context, outboxByID map[int64]domain.YouTubeNotificationOutbox) (messages map[int64]string, failures map[int64]bool) {
	messages = make(map[int64]string, len(outboxByID))
	failures = make(map[int64]bool)
	for id := range outboxByID {
		item := outboxByID[id]
		msg, err := d.formatter.formatMessage(ctx, item)
		if err != nil {
			d.logger.Warn("Failed to pre-format outbox message",
				slog.Int64("outbox_id", id),
				slog.Any("error", err))
			failures[id] = true
			continue
		}
		messages[id] = msg
	}
	return
}

func (d *SendEngine) sendDeliveryMessage(ctx context.Context, req deliverySendRequest) error {
	if err := validateDeliverySendRequest(req); err != nil {
		return err
	}

	sendCtx := ctx
	cancel := func() {}
	if d.config.DeliverySendTimeout > 0 {
		sendCtx, cancel = context.WithTimeoutCause(ctx, d.config.DeliverySendTimeout, errDeliverySendTimeout)
	}
	defer cancel()

	var err error
	if sender, ok := d.sender.(messagedelivery.ClientRequestMessageSender); ok {
		err = sender.SendMessageWithClientRequestID(sendCtx, req.roomID, req.message, deliveryClientRequestID(req.roomID, req.dedupeKeys))
	} else {
		err = d.sender.SendMessage(sendCtx, req.roomID, req.message)
	}
	if err != nil {
		if errors.Is(context.Cause(sendCtx), errDeliverySendTimeout) {
			return fmt.Errorf("send delivery message timed out after %s: %w", d.config.DeliverySendTimeout, errors.Join(errDeliverySendTimeout, err))
		}
		return fmt.Errorf("send delivery message: %w", err)
	}

	return nil
}

func (d *SendEngine) deliveryParallelism() int {
	if d.config.DeliveryParallelism > 0 {
		return d.config.DeliveryParallelism
	}
	return DefaultConfig().DeliveryParallelism
}
