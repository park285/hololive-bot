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
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache/claim"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
)

func (d *SendEngine) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) dispatchstate.DispatchResult {
	result := dispatchstate.DispatchResult{
		SuccessDeliveryIDs: make([]int64, 0, len(rows)),
		TouchedOutboxIDs:   make([]int64, 0, len(rows)),
		SuccessClaimTokens: make([]dispatchstate.ClaimToken, 0, len(rows)),
		FailureBuckets:     make(map[string][]int64),
	}
	var mu sync.Mutex
	reuseCache := claim.NewMemoryDecisionCache()

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

func (d *SendEngine) dispatchGroup(
	ctx context.Context,
	group deliveryGroup,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache claim.DecisionCache,
	result *dispatchstate.DispatchResult,
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

	claimSelection := d.claims.selectClaimedDeliveries(ctx, validRows, validOutboxes, reuseCache)
	d.claims.applyClaimSelection(result, mu, claimSelection)
	validRows = claimSelection.sendRows
	validOutboxes = claimSelection.sendOutboxes
	if len(validRows) == 0 {
		return
	}

	d.dispatchClaimedGroup(ctx, group, validRows, validOutboxes, formattedMessages, formatFailures, claimSelection, result, mu)
}

func (d *SendEngine) dispatchClaimedGroup(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimSelection deliveryClaimSelection,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	if len(validRows) == 1 {
		if d.dispatchClaimedRowsWithKaringIfSupported(ctx, group.roomID, group.channelID, group.kind, validRows, validOutboxes, claimSelection.claimTokens, "per_room", result, mu) {
			return
		}
		d.dispatchClaimedDeliveryRow(ctx, validRows[0], validOutboxes[0], formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)
		return
	}

	if d.dispatchClaimedRowsWithKaringIfSupported(ctx, group.roomID, group.channelID, group.kind, validRows, validOutboxes, claimSelection.claimTokens, "grouped", result, mu) {
		return
	}

	message, formatted := d.formatGroupedMessage(ctx, group, validRows, validOutboxes)
	if !formatted {
		d.dispatchClaimedRowsIndividually(ctx, validRows, validOutboxes, formattedMessages, formatFailures, claimSelection.rowClaimTokens, result, mu)
		return
	}

	d.dispatchGroupedClaimedRows(ctx, group, validRows, validOutboxes, message, claimSelection.claimTokens, result, mu)
}

func (d *SendEngine) dispatchDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	reuseCache claim.DecisionCache,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	outbox, ok := outboxByID[row.OutboxID]
	if !ok {
		d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
		return
	}

	claimSelection := d.claims.selectClaimedDeliveries(ctx, []domain.YouTubeNotificationDelivery{row}, []domain.YouTubeNotificationOutbox{outbox}, reuseCache)
	d.claims.applyClaimSelection(result, mu, claimSelection)
	if len(claimSelection.sendRows) == 0 {
		return
	}
	if d.dispatchClaimedRowsWithKaringIfSupported(ctx, row.RoomID, outbox.ChannelID, outbox.Kind, claimSelection.sendRows, claimSelection.sendOutboxes, claimSelection.claimTokens, "per_room", result, mu) {
		return
	}

	d.dispatchClaimedDeliveryRow(ctx, claimSelection.sendRows[0], claimSelection.sendOutboxes[0], formattedMessages, formatFailures, claimSelection.claimTokens, result, mu)
}

func (d *SendEngine) dispatchClaimedDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
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

func (d *SendEngine) dispatchGroupedClaimedRows(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	message string,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
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
