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
	"log/slog"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *SendEngine) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) dispatchstate.DispatchResult {
	claims := newBatchClaimResolver(d.claims)
	batch := *d

	// 실패 기록 wrapper가 d.claims로 해제를 요청하므로 배치 resolver로 바꾸면 해제가 배치 종료까지 모인다.
	batch.claims = claims
	d = &batch

	defer claims.releaseFinished(ctx)

	result := dispatchstate.DispatchResult{
		SuccessDeliveryIDs: make([]int64, 0, len(rows)),
		TouchedOutboxIDs:   make([]int64, 0, len(rows)),
		SuccessClaimTokens: make([]dispatchstate.ClaimToken, 0, len(rows)),
		FailureBuckets:     make(map[string][]int64),
	}

	var mu sync.Mutex

	reuseCache := newClaimDecisionCache()

	formattedMessages, formatFailures := d.preFormatMessages(ctx, outboxByID)

	frozenGroups, remaining, err := d.frozenDeliveryGroups(ctx, rows, outboxByID)
	if err != nil {
		d.logger.Error("Failed to load persisted delivery requests", slog.Any("error", err))

		return result
	}

	groups, orphanRows := groupDeliveryRows(remaining, outboxByID)

	groups = append(frozenGroups, groups...)

	// orphan row 처리
	for i := range orphanRows {
		d.recordDeliveryFailure(&result, &mu, "outbox row not found", orphanRows[i].ID, orphanRows[i].OutboxID)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.config.DeliveryParallelism)

	for i := range groups {
		group := &groups[i]

		eg.Go(func() error {
			return panicguard.RunE(d.logger, panicguard.BackgroundTask, "youtube-outbox-delivery-group", func() error {
				d.dispatchGroup(egCtx, group, formattedMessages, formatFailures, reuseCache, &result, &mu)

				return nil
			})
		})
	}

	if err := eg.Wait(); err != nil {
		d.logger.Warn("Delivery dispatch worker failed", slog.Any("error", err))
	}

	return result
}

func (d *SendEngine) dispatchGroup(
	ctx context.Context,
	group *deliveryGroup,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *claimDecisionCache,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	if group.frozen != nil {
		d.dispatchFrozenGroup(ctx, group, formattedMessages, formatFailures, reuseCache, result, mu)

		return
	}

	groupOutboxByID := make(map[int64]domain.YouTubeNotificationOutbox, len(group.outboxes))
	for i := range group.outboxes {
		groupOutboxByID[group.outboxes[i].ID] = group.outboxes[i]
	}

	// 단건 그룹: 기존 개별 dispatch 경로
	if len(group.rows) == 1 {
		d.dispatchDeliveryRow(ctx, &group.rows[0], groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)

		return
	}

	validRows, validOutboxes, invalidRows := partitionGroupedDeliveries(group)
	d.dispatchRowsIndividually(ctx, invalidRows, groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)

	// 검증 후 1건 이하 -> 개별 dispatch
	if len(validRows) <= 1 {
		d.dispatchRowsIndividually(ctx, validRows, groupOutboxByID, formattedMessages, formatFailures, reuseCache, result, mu)

		return
	}

	claimSelection := d.claims.selectClaimedDeliveries(ctx, validRows, validOutboxes, reuseCache)
	d.applyLifecycleClaimSelection(ctx, &claimSelection, store.DeliveryModeGrouped, result, mu)

	validRows = claimSelection.sendRows
	validOutboxes = claimSelection.sendOutboxes

	if len(validRows) == 0 {
		return
	}

	d.dispatchClaimedGroup(ctx, group, validRows, validOutboxes, formattedMessages, formatFailures, &claimSelection, result, mu)
}

func (d *SendEngine) dispatchClaimedGroup(
	ctx context.Context,
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimSelection *deliveryClaimSelection,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	if len(validRows) == 1 {
		d.dispatchClaimedDeliveryRow(ctx, &validRows[0], &validOutboxes[0], formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)

		return
	}

	message, err := d.formatGroupedMessage(ctx, group, validOutboxes)
	if err != nil {
		if d.applyPreparedLifecycleFailure(ctx, validRows, validOutboxes, lifecycle.FailureRetryable, lifecycleReasonFormat, store.DeliveryModeGrouped, result, mu) {
			d.recordGroupedFormatFailure(ctx, group, validRows, validOutboxes, claimSelection.claimTokens, err, result, mu)
		}

		return
	}

	d.dispatchGroupedClaimedRows(ctx, group, validRows, validOutboxes, message, formattedMessages, formatFailures, claimSelection.claimTokens, claimSelection.rowClaimTokens, result, mu)
}

func (d *SendEngine) dispatchDeliveryRow(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache *claimDecisionCache,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	outbox, ok := outboxByID[row.OutboxID]
	if !ok {
		d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)

		return
	}

	claimSelection := d.claims.selectClaimedDeliveries(ctx, []domain.YouTubeNotificationDelivery{*row}, []domain.YouTubeNotificationOutbox{outbox}, reuseCache)
	d.applyLifecycleClaimSelection(ctx, &claimSelection, store.DeliveryModePerRoom, result, mu)

	if len(claimSelection.sendRows) == 0 {
		return
	}

	d.dispatchClaimedDeliveryRow(ctx, &claimSelection.sendRows[0], &claimSelection.sendOutboxes[0], formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)
}

func (d *SendEngine) dispatchClaimedDeliveryRow(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	rows, outboxes := singleDeliveryBatch(row, outbox)
	sendReq, prepared := d.preparePerRoomSend(
		ctx, row, outbox, rows, outboxes, formattedMessages, formatFailures, claimTokens, result, mu,
	)

	if !prepared {
		return
	}

	sendReq, prepared = d.freezeDeliveryRequest(ctx, rows, sendReq)
	if !prepared {
		return
	}

	operation, begun := d.beginLifecycleOperation(ctx, rows, outboxes, result, mu)
	if !begun {
		return
	}

	attemptStartedAt := time.Now().UTC()
	d.logCommunityShortsDeliveryAttemptStarted(rows, outboxes, attemptStartedAt, "per_room")

	if sendErr := d.sendFrozenDelivery(ctx, operation, sendReq); sendErr != nil {
		d.handlePerRoomSendFailure(
			ctx, operation, row, rows, outboxes, sendReq, claimTokens, sendErr, result, mu,
		)

		return
	}

	if !d.completeLifecycleSent(ctx, operation, claimTokens, store.DeliveryModePerRoom, result, mu) {
		return
	}

	d.recordPerRoomSuccess(row, rows, outboxes, sendReq, claimTokens, result, mu)
}

func (d *SendEngine) preparePerRoomSend(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) (deliverySendRequest, bool) {
	if formatFailures[row.OutboxID] {
		if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailureRetryable, lifecycleReasonFormat, store.DeliveryModePerRoom, result, mu) {
			d.recordPerRoomFormatFailure(ctx, row, rows, outboxes, claimTokens, result, mu)
		}

		return deliverySendRequest{}, false
	}

	message, ok := formattedMessages[row.OutboxID]
	if !ok {
		if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailureRetryable, lifecycleReasonMessage, store.DeliveryModePerRoom, result, mu) {
			d.recordPerRoomMissingMessage(ctx, row, claimTokens, result, mu)
		}

		return deliverySendRequest{}, false
	}

	sendReq, err := buildDeliverySendRequest(row.RoomID, message, outboxes)
	if err != nil {
		if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailurePermanent, lifecycleReasonRequest, store.DeliveryModePerRoom, result, mu) {
			d.recordPerRoomRequestBuildFailure(ctx, row, outbox, rows, outboxes, claimTokens, err, result, mu)
		}

		return deliverySendRequest{}, false
	}

	return sendReq, true
}

func (d *SendEngine) handlePerRoomSendFailure(
	ctx context.Context,
	operation store.StartedOperation,
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	sendErr error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	if !d.applyPerRoomLifecycleFailure(ctx, operation, row, sendReq, sendErr, result, mu) {
		return
	}

	if errors.Is(sendErr, errDeliverySendOutcomeUnknown) {
		d.recordPerRoomSendOutcomeUnknown(row, sendReq, sendErr)

		return
	}

	d.recordPerRoomSendFailure(ctx, row, rows, outboxes, sendReq, claimTokens, sendErr, result, mu)
}

func (d *SendEngine) applyPerRoomLifecycleFailure(
	ctx context.Context,
	operation store.StartedOperation,
	row *domain.YouTubeNotificationDelivery,
	sendReq deliverySendRequest,
	sendErr error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) bool {
	if errors.Is(sendErr, errRequestReissued) {
		appendLifecycleTouched(result, mu, operation.TouchedOutboxIDs())

		return true
	}

	kind, reason, retryAfter := lifecycleProviderFailure(sendErr)
	if kind == lifecycle.FailureOutcomeUnknown {
		d.recordPerRoomSendOutcomeUnknown(row, sendReq, sendErr)

		return false
	}

	return d.applyStartedLifecycleFailure(ctx, operation, kind, reason, retryAfter, store.DeliveryModePerRoom, result, mu)
}

func (d *SendEngine) dispatchGroupedClaimedRows(
	ctx context.Context,
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	message string,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimTokens []dispatchstate.ClaimToken,
	rowClaimTokens [][]dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	sendReq, prepared := d.prepareGroupedSend(ctx, group, validRows, validOutboxes, message, claimTokens, result, mu)
	if !prepared {
		return
	}

	sendReq, prepared = d.freezeDeliveryRequest(ctx, validRows, sendReq)
	if !prepared {
		return
	}

	operation, begun := d.beginLifecycleOperation(ctx, validRows, validOutboxes, result, mu)
	if !begun {
		return
	}

	attemptStartedAt := time.Now().UTC()
	d.logCommunityShortsDeliveryAttemptStarted(validRows, validOutboxes, attemptStartedAt, "grouped")

	if sendErr := d.sendFrozenDelivery(ctx, operation, sendReq); sendErr != nil {
		d.handleGroupedSendFailure(
			ctx, operation, group, validRows, validOutboxes, sendReq, formattedMessages, formatFailures,
			claimTokens, rowClaimTokens, sendErr, result, mu,
		)

		return
	}

	if !d.completeLifecycleSent(ctx, operation, claimTokens, store.DeliveryModeGrouped, result, mu) {
		return
	}

	d.recordGroupedSuccess(group, validRows, validOutboxes, sendReq, claimTokens, result, mu)
}

func (d *SendEngine) prepareGroupedSend(
	ctx context.Context,
	group *deliveryGroup,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	message string,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) (deliverySendRequest, bool) {
	sendReq, err := buildDeliverySendRequest(group.roomID, message, outboxes)
	if err == nil {
		return sendReq, true
	}

	if d.applyPreparedLifecycleFailure(ctx, rows, outboxes, lifecycle.FailurePermanent, lifecycleReasonRequest, store.DeliveryModeGrouped, result, mu) {
		d.recordGroupedRequestBuildFailure(ctx, group, rows, outboxes, claimTokens, err, result, mu)
	}

	return deliverySendRequest{}, false
}

func (d *SendEngine) handleGroupedSendFailure(
	ctx context.Context,
	operation store.StartedOperation,
	group *deliveryGroup,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimTokens []dispatchstate.ClaimToken,
	rowClaimTokens [][]dispatchstate.ClaimToken,
	sendErr error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	if errors.Is(sendErr, errRequestReissued) {
		appendLifecycleTouched(result, mu, operation.TouchedOutboxIDs())
		d.recordGroupedSendFailure(ctx, group, rows, outboxes, sendReq, claimTokens, sendErr, result, mu)

		return
	}

	kind, reason, retryAfter := lifecycleProviderFailure(sendErr)
	if kind == lifecycle.FailureOutcomeUnknown {
		d.recordGroupedSendOutcomeUnknown(group, rows, sendReq, sendErr)

		return
	}

	if kind == lifecycle.FailurePermanent && shouldFallbackGroupedSend(sendErr) {
		d.logGroupedSendFallback(group, rows, sendReq, sendErr)
		d.dispatchStartedRowsIndividually(
			ctx, operation, rows, outboxes, formattedMessages, formatFailures, rowClaimTokens, result, mu,
		)

		return
	}

	if !d.applyStartedLifecycleFailure(ctx, operation, kind, reason, retryAfter, store.DeliveryModeGrouped, result, mu) {
		return
	}

	d.recordGroupedSendFailure(ctx, group, rows, outboxes, sendReq, claimTokens, sendErr, result, mu)
}

func (d *SendEngine) logGroupedSendFallback(
	group *deliveryGroup,
	rows []domain.YouTubeNotificationDelivery,
	sendReq deliverySendRequest,
	sendErr error,
) {
	d.logger.Warn("Grouped delivery send failed, falling back to version-fenced individual deliveries",
		slog.String("room_id", group.roomID),
		slog.String("channel_id", group.channelID),
		slog.String("kind", string(group.kind)),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
}

func (d *SendEngine) dispatchStartedRowsIndividually(
	ctx context.Context,
	operation store.StartedOperation,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	rowClaimTokens [][]dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	requests, err := d.prepareFallbackRequests(ctx, rows, outboxes, formattedMessages, formatFailures)
	if err != nil {
		observeGroupedSendFallback(groupedSendFallbackResultPrepareFailed)
		d.logger.Warn("Failed to prepare grouped fallback", slog.Any("error", err))

		return
	}

	frozen, err := d.transition.FreezeFallbackRequests(ctx, operation, requests)
	if err != nil {
		observeGroupedSendFallback(groupedSendFallbackResultFreezeFailed)
		d.logger.Error("Failed to freeze grouped fallback", slog.Any("error", err))

		return
	}

	observeGroupedSendFallback(groupedSendFallbackResultStarted)

	byID := make(map[int64]store.FrozenRequest, len(frozen))
	for i := range frozen {
		byID[frozen[i].MemberIDs[0]] = frozen[i]
	}

	for i := range rows {
		rowOperation, err := operation.ForOwner(rows[i].ID)
		if err != nil {
			return
		}

		req, err := deliveryRequestFromFrozen(byID[rows[i].ID])
		if err != nil {
			return
		}

		var tokens []dispatchstate.ClaimToken

		if i < len(rowClaimTokens) {
			tokens = rowClaimTokens[i]
		}

		rowBatch, outboxBatch := singleDeliveryBatch(&rows[i], &outboxes[i])

		if sendErr := d.sendFrozenDelivery(ctx, rowOperation, req); sendErr != nil {
			d.handlePerRoomSendFailure(ctx, rowOperation, &rows[i], rowBatch, outboxBatch, req, tokens, sendErr, result, mu)

			continue
		}

		if d.completeLifecycleSent(ctx, rowOperation, tokens, store.DeliveryModePerRoom, result, mu) {
			d.recordPerRoomSuccess(&rows[i], rowBatch, outboxBatch, req, tokens, result, mu)
		}
	}
}
