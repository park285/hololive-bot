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
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func (d *ClaimManager) claimOutboxBatch(ctx context.Context) ([]domain.YouTubeNotificationOutbox, error) {
	if d == nil || d.fanout == nil {
		return nil, errors.New("claim outboxes for fanout: lifecycle fanout service is unavailable")
	}

	out, err := d.fanout.Claim(ctx)
	if err != nil {
		return nil, fmt.Errorf("claim outboxes for fanout: %w", err)
	}

	return out, nil
}

func (d *ClaimManager) processPerRoomBatch(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox) int {
	if d == nil || d.fanout == nil {
		return 0
	}

	stats := d.fanout.materialize(ctx, outboxItems)
	d.recordOutboxEnqueueStats(len(outboxItems), stats)

	return d.processPendingDeliveries(ctx)
}

func (d *ClaimManager) reconcileTerminalOutboxStatuses(ctx context.Context) {
	if d == nil || d.delivery == nil {
		return
	}

	projector := d.projector
	if projector == nil {
		projector = newOutboxAggregateProjector(d.delivery)
	}

	outboxIDs, err := projector.ProjectPending(ctx, d.config.BatchSize)
	if err != nil {
		d.logger.Warn("Failed to reconcile pending outbox aggregate statuses", slog.Any("error", err))

		return
	}

	if len(outboxIDs) == 0 {
		return
	}

	if err := d.logFinalizedCommunityShortsOutboxResults(ctx, outboxIDs); err != nil {
		d.logger.Warn("Failed to log finalized community/shorts outbox results", slog.Any("error", err))

		return
	}

	d.logger.Info("Recovered outbox aggregate statuses from persisted delivery rows",
		slog.Int("outbox_count", len(outboxIDs)))
}

type outboxEnqueueStats struct {
	enqueuedOutboxes         int
	noSubscriberOutboxes     int
	subscriberLookupFailures int
	enqueueFailures          int
	totalTargetRooms         int
}

func (s *outboxEnqueueStats) add(next outboxEnqueueStats) {
	s.enqueuedOutboxes += next.enqueuedOutboxes
	s.noSubscriberOutboxes += next.noSubscriberOutboxes
	s.subscriberLookupFailures += next.subscriberLookupFailures
	s.enqueueFailures += next.enqueueFailures
	s.totalTargetRooms += next.totalTargetRooms
}

func deliveryRoomIDs(rooms map[string]bool) []string {
	roomIDs := make([]string, 0, len(rooms))
	for roomID := range rooms {
		roomIDs = append(roomIDs, roomID)
	}

	return roomIDs
}

func (d *ClaimManager) recordOutboxEnqueueStats(claimed int, stats outboxEnqueueStats) {
	if claimed > 0 || stats.enqueuedOutboxes > 0 || stats.noSubscriberOutboxes > 0 || stats.subscriberLookupFailures > 0 || stats.enqueueFailures > 0 || stats.totalTargetRooms > 0 {
		d.logger.Info("Outbox per-room enqueue completed",
			slog.Int("outbox_claimed", claimed),
			slog.Int("outbox_enqueued", stats.enqueuedOutboxes),
			slog.Int("outbox_no_subscribers", stats.noSubscriberOutboxes),
			slog.Int("subscriber_lookup_failures", stats.subscriberLookupFailures),
			slog.Int("enqueue_failures", stats.enqueueFailures),
			slog.Int("target_rooms", stats.totalTargetRooms))
	}

	observeOutboxEnqueueOutboxes("claimed", claimed)
	observeOutboxEnqueueOutboxes("enqueued", stats.enqueuedOutboxes)
	observeOutboxEnqueueOutboxes("no_subscribers", stats.noSubscriberOutboxes)
	observeOutboxEnqueueOutboxes("subscriber_lookup_failures", stats.subscriberLookupFailures)
	observeOutboxEnqueueOutboxes("enqueue_failures", stats.enqueueFailures)
	observeOutboxEnqueueTargetRooms(stats.totalTargetRooms)
}

func (d *ClaimManager) processPendingDeliveries(ctx context.Context) int {
	if d == nil || d.transition == nil {
		return 0
	}

	return d.processPendingDeliveriesWithLifecycle(ctx)
}

func (d *ClaimManager) processPendingDeliveriesWithLifecycle(ctx context.Context) int {
	rows, err := d.transition.ClaimPending(ctx, d.config.BatchSize)
	if err != nil {
		d.logger.Error("Failed to claim version-fenced delivery rows", slog.Any("error", err))

		return 0
	}

	if len(rows) == 0 {
		return 0
	}

	startedAt := time.Now()

	defer func() {
		observeOutboxDispatchDuration(time.Since(startedAt))
	}()

	observeOutboxDeliveryClaimed(len(rows))
	observeOutboxDispatchBatchSize(len(rows))

	outboxByID, err := d.loadOutboxItemsByIDs(ctx, collectDeliveryOutboxIDs(rows))
	if err != nil {
		d.logger.Error("Failed to load outbox rows for version-fenced deliveries; preserving exact claims",
			slog.Any("error", err),
			slog.Int("delivery_count", len(rows)))

		return len(rows)
	}

	prepared, err := d.transition.PrepareClaimed(ctx, rows, outboxByID)
	if err != nil {
		d.logger.Error("Failed to prepare logical delivery groups; preserving exact claims",
			slog.Any("error", err),
			slog.Int("delivery_count", len(rows)))

		return len(rows)
	}

	observeLogicalResolutions(prepared.Resolutions)

	for i := range prepared.Blocked {
		d.logger.Error("Blocked invalid logical delivery group",
			slog.String("logical_key_hash", prepared.Blocked[i].KeyHash),
			slog.String("invariant_reason", string(prepared.Blocked[i].Reason)))
	}

	result := dispatchstate.DispatchResult{
		SuccessDeliveryIDs: make([]int64, 0, len(prepared.ActiveRows)),
		TouchedOutboxIDs:   append([]int64(nil), prepared.TouchedOutboxIDs...),
		SuccessClaimTokens: make([]dispatchstate.ClaimToken, 0, len(prepared.ActiveRows)),
		FailureBuckets:     make(map[string][]int64),
	}
	if len(prepared.ActiveRows) > 0 {
		result = d.dispatchDeliveryRows(ctx, prepared.ActiveRows, outboxByID)
		result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, prepared.TouchedOutboxIDs...)
	}

	touchedOutboxIDs := deliverysql.UniqueInt64s(result.TouchedOutboxIDs)
	aggregateFailures := d.reconcileTouchedOutboxes(ctx, touchedOutboxIDs)
	d.recordOutboxDispatchResult(len(rows), &result, touchedOutboxIDs, aggregateFailures)

	return len(rows)
}

func (d *ClaimManager) reconcileTouchedOutboxes(ctx context.Context, touchedOutboxIDs []int64) int {
	projector := d.projector
	if projector == nil {
		projector = newOutboxAggregateProjector(d.delivery)
	}

	if err := projector.Project(ctx, touchedOutboxIDs); err != nil {
		d.logger.Warn("Failed to update outbox aggregate statuses", slog.Any("error", err))

		return 1
	} else if err := d.logFinalizedCommunityShortsOutboxResults(ctx, touchedOutboxIDs); err != nil {
		d.logger.Warn("Failed to log finalized community/shorts outbox results", slog.Any("error", err))
	}

	return 0
}

func (d *ClaimManager) recordOutboxDispatchResult(
	claimed int,
	result *dispatchstate.DispatchResult,
	touchedOutboxIDs []int64,
	aggregateFailures int,
) {
	observeOutboxDeliveryProcessed("sent", len(result.SuccessDeliveryIDs))
	observeOutboxDeliveryProcessed("failed", result.FailedDeliveries)
	observeOutboxDispatchTouchedOutboxes(len(touchedOutboxIDs))

	d.logger.Info("Outbox per-room dispatch completed",
		slog.Int("delivery_claimed", claimed),
		slog.Int("delivery_sent", len(result.SuccessDeliveryIDs)),
		slog.Int("delivery_failed", result.FailedDeliveries),
		slog.Int("outbox_touched", len(touchedOutboxIDs)),
		slog.Int("aggregate_failures", aggregateFailures))
}

func collectDeliveryOutboxIDs(rows []domain.YouTubeNotificationDelivery) []int64 {
	outboxIDs := make([]int64, 0, len(rows))
	for i := range rows {
		outboxIDs = append(outboxIDs, rows[i].OutboxID)
	}

	return outboxIDs
}

func collectDeliveryIDs(rows []domain.YouTubeNotificationDelivery) []int64 {
	deliveryIDs := make([]int64, 0, len(rows))
	for i := range rows {
		deliveryIDs = append(deliveryIDs, rows[i].ID)
	}

	return deliveryIDs
}

func (d *ClaimManager) loadOutboxItemsByIDs(ctx context.Context, ids []int64) (map[int64]domain.YouTubeNotificationOutbox, error) {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return map[int64]domain.YouTubeNotificationOutbox{}, nil
	}

	var rows []domain.YouTubeNotificationOutbox

	if err := deliverysql.SelectDeliverySQL(ctx, d.db, &rows, "load outbox rows by ids", mustSQL("dispatcher_claim_0370_02.sql")+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
	`, deliverysql.AppendDeliveryInt64Args(nil, uniqueIDs)...); err != nil {
		return nil, fmt.Errorf("load outbox rows by ids: %w", err)
	}

	result := make(map[int64]domain.YouTubeNotificationOutbox, len(rows))
	for i := range rows {
		result[rows[i].ID] = rows[i]
	}

	return result, nil
}
