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
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type deliveryDispatchResult struct {
	successDeliveryIDs []int64
	touchedOutboxIDs   []int64
	successClaimTokens []deliveryClaimToken
	failedDeliveries   int
	failureBuckets     map[string][]int64
}

func (d *Dispatcher) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) deliveryDispatchResult {
	result := deliveryDispatchResult{
		successDeliveryIDs: make([]int64, 0, len(rows)),
		touchedOutboxIDs:   make([]int64, 0, len(rows)),
		successClaimTokens: make([]deliveryClaimToken, 0, len(rows)),
		failureBuckets:     make(map[string][]int64),
	}
	var mu sync.Mutex
	reuseCache := newDeliveryClaimReuseCache(len(rows))

	formattedMessages, formatFailures := d.preFormatMessages(ctx, outboxByID)

	groups, orphanRows := groupDeliveryRows(rows, outboxByID)

	// orphan row 처리
	for i := range orphanRows {
		d.recordDeliveryFailure(&result, &mu, "outbox row not found", orphanRows[i].ID, orphanRows[i].OutboxID)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.deliveryParallelism())

	for i := range groups {
		group := groups[i]
		eg.Go(func() error {
			d.dispatchGroup(egCtx, group, formattedMessages, formatFailures, reuseCache, &result, &mu)
			return nil
		})
	}
	_ = eg.Wait()

	return result
}

func (d *Dispatcher) dispatchGroup(
	ctx context.Context,
	group deliveryGroup,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *deliveryClaimReuseCache,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	groupOutboxByID := make(map[int64]domain.YouTubeNotificationOutbox, len(group.outboxes))
	for i := range group.outboxes {
		groupOutboxByID[group.outboxes[i].ID] = group.outboxes[i]
	}

	// 단건 그룹: 기존 개별 dispatch 경로
	if len(group.rows) == 1 {
		d.dispatchDeliveryRow(ctx, group.rows[0], groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)
		return
	}

	validRows, validOutboxes, invalidRows := partitionGroupedDeliveries(group)
	d.dispatchRowsIndividually(ctx, invalidRows, groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)

	// 검증 후 1건 이하 -> 개별 dispatch
	if len(validRows) <= 1 {
		d.dispatchRowsIndividually(ctx, validRows, groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)
		return
	}

	claimSelection := d.selectClaimedDeliveries(ctx, validRows, validOutboxes, reuseCache)
	d.applyClaimSelection(result, mu, claimSelection)
	validRows = claimSelection.sendRows
	validOutboxes = claimSelection.sendOutboxes
	if len(validRows) == 0 {
		return
	}
	if len(validRows) == 1 {
		d.dispatchClaimedDeliveryRow(ctx, validRows[0], validOutboxes[0], formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)
		return
	}

	message, formatted := d.formatGroupedMessage(ctx, group, validRows, validOutboxes)
	if !formatted {
		d.dispatchClaimedRowsIndividually(ctx, validRows, validOutboxes, formattedMessages, formatFailures, claimSelection.rowClaimTokens, result, mu)
		return
	}

	d.dispatchGroupedClaimedRows(ctx, group, validRows, validOutboxes, message, claimSelection.claimTokens, result, mu)
}

func (d *Dispatcher) dispatchDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *deliveryClaimReuseCache,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	outbox, ok := outboxByID[row.OutboxID]
	if !ok {
		d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
		return
	}

	claimSelection := d.selectClaimedDeliveries(ctx, []domain.YouTubeNotificationDelivery{row}, []domain.YouTubeNotificationOutbox{outbox}, reuseCache)
	d.applyClaimSelection(result, mu, claimSelection)
	if len(claimSelection.sendRows) == 0 {
		return
	}

	d.dispatchClaimedDeliveryRow(ctx, claimSelection.sendRows[0], claimSelection.sendOutboxes[0], formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)
}

func (d *Dispatcher) dispatchClaimedDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	rows, outboxes := singleDeliveryBatch(row, outbox)
	if formatFailures[row.OutboxID] {
		d.recordPerRoomFormatFailure(ctx, row, rows, outboxes, claimTokens, result, mu)
		return
	}

	message, ok := formattedMessages[row.OutboxID]
	if !ok {
		d.recordPerRoomMissingMessage(ctx, row, claimTokens, result, mu)
		return
	}

	sendReq, err := buildDeliverySendRequest(row.RoomID, message, []domain.YouTubeNotificationOutbox{outbox})
	if err != nil {
		d.recordPerRoomRequestBuildFailure(ctx, row, outbox, rows, outboxes, claimTokens, err, result, mu)
		return
	}

	attemptStartedAt := time.Now().UTC()
	d.logCommunityShortsDeliveryAttemptStarted(rows, outboxes, attemptStartedAt, "per_room")
	if sendErr := d.sendDeliveryMessage(ctx, sendReq); sendErr != nil {
		d.recordPerRoomSendFailure(ctx, row, rows, outboxes, sendReq, claimTokens, sendErr, result, mu)
		return
	}

	d.recordPerRoomSuccess(ctx, row, rows, outboxes, sendReq, claimTokens, result, mu)
}

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

func (d *Dispatcher) dispatchRowsIndividually(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *deliveryClaimReuseCache,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	for i := range rows {
		d.dispatchDeliveryRow(ctx, rows[i], outboxByID, formattedMessages, formatFailures, reuseCache, result, mu)
	}
}

func (d *Dispatcher) formatGroupedMessage(
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

func (d *Dispatcher) dispatchClaimedRowsIndividually(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	rowClaimTokens [][]deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	for i := range rows {
		var claims []deliveryClaimToken
		if i < len(rowClaimTokens) {
			claims = rowClaimTokens[i]
		}
		d.dispatchClaimedDeliveryRow(ctx, rows[i], outboxes[i], formattedMessages, formatFailures, claims, result, mu)
	}
}

func (d *Dispatcher) dispatchGroupedClaimedRows(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	message string,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	sendReq, err := buildDeliverySendRequest(group.roomID, message, validOutboxes)
	if err != nil {
		d.recordGroupedRequestBuildFailure(ctx, group, validRows, validOutboxes, claimTokens, err, result, mu)
		return
	}

	attemptStartedAt := time.Now().UTC()
	d.logCommunityShortsDeliveryAttemptStarted(validRows, validOutboxes, attemptStartedAt, "grouped")
	if sendErr := d.sendDeliveryMessage(ctx, sendReq); sendErr != nil {
		d.recordGroupedSendFailure(ctx, group, validRows, validOutboxes, sendReq, claimTokens, sendErr, result, mu)
		return
	}

	d.recordGroupedSuccess(ctx, group, validRows, validOutboxes, sendReq, claimTokens, result, mu)
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

func singleDeliveryBatch(
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
) ([]domain.YouTubeNotificationDelivery, []domain.YouTubeNotificationOutbox) {
	return []domain.YouTubeNotificationDelivery{row}, []domain.YouTubeNotificationOutbox{outbox}
}

func (d *Dispatcher) releaseDeliveryClaimsWithWarning(
	ctx context.Context,
	claims []deliveryClaimToken,
	message string,
	attrs ...any,
) {
	if releaseErr := d.releaseDeliveryClaims(ctx, claims); releaseErr != nil && d.logger != nil {
		d.logger.Warn(message, append(attrs, slog.Any("error", releaseErr))...)
	}
}

// preFormatMessages: outbox_id별로 메시지를 1회 포맷하여 캐싱
func (d *Dispatcher) preFormatMessages(ctx context.Context, outboxByID map[int64]domain.YouTubeNotificationOutbox) (messages map[int64]string, failures map[int64]bool) {
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

func (d *Dispatcher) sendDeliveryMessage(ctx context.Context, req deliverySendRequest) error {
	if err := validateDeliverySendRequest(req); err != nil {
		return err
	}

	sendCtx := ctx
	cancel := func() {}
	if d.cfg.DeliverySendTimeout > 0 {
		sendCtx, cancel = context.WithTimeoutCause(ctx, d.cfg.DeliverySendTimeout, errDeliverySendTimeout)
	}
	defer cancel()

	if err := d.sender.SendMessage(sendCtx, req.roomID, req.message); err != nil {
		if errors.Is(context.Cause(sendCtx), errDeliverySendTimeout) {
			return fmt.Errorf("send delivery message timed out after %s: %w", d.cfg.DeliverySendTimeout, errors.Join(errDeliverySendTimeout, err))
		}
		return fmt.Errorf("send delivery message: %w", err)
	}

	return nil
}

func (d *Dispatcher) deliveryParallelism() int {
	if d.cfg.DeliveryParallelism > 0 {
		return d.cfg.DeliveryParallelism
	}
	return DefaultConfig().DeliveryParallelism
}
