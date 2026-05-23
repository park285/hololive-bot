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
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *Dispatcher) recordPerRoomFormatFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after format error",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", "format message", nil)
	d.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "format message")
	d.recordDeliveryFailure(result, mu, "format message", row.ID, row.OutboxID)
}

func (d *Dispatcher) recordPerRoomMissingMessage(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after missing preformatted message",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
}

func (d *Dispatcher) recordPerRoomRequestBuildFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after request build error",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	d.logger.Warn("Failed to build per-room delivery request",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttrForOutboxes([]domain.YouTubeNotificationOutbox{outbox}),
		slog.Any("error", err))
	d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", "dedupe key", err)
	d.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "dedupe key")
	d.recordDeliveryFailure(result, mu, "dedupe key", row.ID, row.OutboxID)
}

func (d *Dispatcher) recordPerRoomSendFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	sendErr error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after send failure",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	reason := deliveryFailureReason(sendErr)
	d.logger.Warn("Failed to send per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
	d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", reason, sendErr)
	d.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", reason)
	d.recordDeliveryFailure(result, mu, reason, row.ID, row.OutboxID)
}

func (d *Dispatcher) recordPerRoomSuccess(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	sentAt := time.Now()
	d.logger.Info("Sent per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	d.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, sentAt, "per_room", "success", "", nil)
	d.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, "per_room", "success", "")

	mu.Lock()
	result.successDeliveryIDs = append(result.successDeliveryIDs, row.ID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, row.OutboxID)
	result.successClaimTokens = append(result.successClaimTokens, claimTokens...)
	mu.Unlock()
}

func (d *Dispatcher) recordDeliveryFailure(
	result *deliveryDispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	mu.Lock()
	result.failedDeliveries++
	result.failureBuckets[reason] = append(result.failureBuckets[reason], deliveryID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, outboxID)
	mu.Unlock()
}

func (d *Dispatcher) recordGroupedRequestBuildFailure(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after request build error",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
	)
	failedAt := time.Now()
	d.logger.Warn("Failed to build grouped delivery request",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(validOutboxes)),
		dedupeKeyLogAttrForOutboxes(validOutboxes),
		slog.Any("error", err))
	d.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key", err)
	d.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key")
	for i := range validRows {
		d.recordDeliveryFailure(result, mu, "dedupe key", validRows[i].ID, validRows[i].OutboxID)
	}
}

func (d *Dispatcher) recordGroupedSendFailure(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	sendErr error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after send failure",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
	)
	failedAt := time.Now()
	reason := deliveryFailureReason(sendErr)
	d.logger.Warn("Failed to send grouped delivery",
		slog.String("room_id", group.roomID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
	d.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, failedAt, "grouped", "failure", reason, sendErr)
	d.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", reason)
	for i := range validRows {
		d.recordDeliveryFailure(result, mu, reason, validRows[i].ID, validRows[i].OutboxID)
	}
}

func (d *Dispatcher) recordGroupedSuccess(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	sentAt := time.Now()
	d.logger.Info("Sent grouped delivery",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	d.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, sentAt, "grouped", "success", "", nil)
	d.logCommunityShortsDeliveryResult(validRows, validOutboxes, sentAt, "grouped", "success", "")

	mu.Lock()
	for i := range validRows {
		result.successDeliveryIDs = append(result.successDeliveryIDs, validRows[i].ID)
		result.touchedOutboxIDs = append(result.touchedOutboxIDs, validRows[i].OutboxID)
	}
	result.successClaimTokens = append(result.successClaimTokens, claimTokens...)
	mu.Unlock()
}
