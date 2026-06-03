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

package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
)

func (d *ClaimManager) claimOutboxBatch(ctx context.Context) ([]domain.YouTubeNotificationOutbox, error) {
	return d.fetchAndLockForPerRoom(ctx)
}

// fetchAndLockForPerRoom: delivery row가 아직 없는 outbox만 claim
func (d *ClaimManager) fetchAndLockForPerRoom(ctx context.Context) ([]domain.YouTubeNotificationOutbox, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}

	now := time.Now()
	lockExpiry := now.Add(-d.config.LockTimeout)

	rows, err := d.db.Query(ctx, `
		WITH claim AS (
			SELECT o.id
			FROM youtube_notification_outbox o
			WHERE o.status = $1
			  AND (o.locked_at IS NULL OR o.locked_at < $2)
			  AND o.next_attempt_at <= $3
			  AND NOT EXISTS (
				SELECT 1 FROM youtube_notification_delivery d
				WHERE d.outbox_id = o.id
			  )
			ORDER BY o.created_at ASC
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		),
		updated AS (
			UPDATE youtube_notification_outbox o
			SET locked_at = $5
			FROM claim
			WHERE o.id = claim.id
				RETURNING o.id, o.kind, o.channel_id, o.content_id, o.payload::text AS payload, o.status,
				          o.attempt_count, o.next_attempt_at, o.created_at, o.locked_at, o.sent_at, COALESCE(o.error, '') AS error
			)
			SELECT id, kind, channel_id, content_id, payload, status,
		       attempt_count, next_attempt_at, created_at, locked_at, sent_at, error
		FROM updated
		ORDER BY created_at ASC
	`, domain.OutboxStatusPending, lockExpiry, now, d.config.BatchSize, now)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock outbox items for per-room mode: %w", err)
	}
	defer rows.Close()
	items, err := pgx.CollectRows(rows, deliverysql.ScanOutboxRow)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock outbox items for per-room mode: %w", err)
	}

	return items, nil
}

func (d *ClaimManager) processPerRoomBatch(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox) int {
	roomsByChannel := d.collectRoomsByChannel(ctx, outboxItems)
	d.enqueueDeliveries(ctx, outboxItems, roomsByChannel)
	return d.processPendingDeliveries(ctx)
}

func (d *ClaimManager) reconcileTerminalOutboxStatuses(ctx context.Context) {
	if d == nil || d.delivery == nil {
		return
	}

	outboxIDs, err := d.delivery.FindPendingOutboxIDsForAggregateSync(ctx, d.config.BatchSize)
	if err != nil {
		d.logger.Warn("Failed to find terminal outbox rows for aggregate sync", slog.Any("error", err))
		return
	}
	if len(outboxIDs) == 0 {
		return
	}

	if err := d.delivery.UpdateOutboxAggregateStatuses(ctx, outboxIDs); err != nil {
		d.logger.Warn("Failed to reconcile outbox aggregate statuses", slog.Any("error", err))
		return
	}
	if err := d.logFinalizedCommunityShortsOutboxResults(ctx, outboxIDs); err != nil {
		d.logger.Warn("Failed to log finalized community/shorts outbox results", slog.Any("error", err))
		return
	}

	d.logger.Info("Recovered outbox aggregate statuses from persisted delivery rows",
		slog.Int("outbox_count", len(outboxIDs)))
}

func (d *ClaimManager) enqueueDeliveries(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox, roomsByChannel map[string]channelAlarmRoomTargets) {
	startedAt := time.Now()
	defer func() {
		outboxEnqueueDuration.Observe(time.Since(startedAt).Seconds())
	}()

	stats := outboxEnqueueStats{}

	for i := range outboxItems {
		stats.add(d.enqueueDelivery(ctx, &outboxItems[i], roomsByChannel))
	}

	d.recordOutboxEnqueueStats(len(outboxItems), stats)
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

func (d *ClaimManager) enqueueDelivery(
	ctx context.Context,
	item *domain.YouTubeNotificationOutbox,
	roomsByChannel map[string]channelAlarmRoomTargets,
) outboxEnqueueStats {
	rooms, ok := roomsForItem(roomsByChannel, *item)
	if !ok {
		d.markFailed(ctx, item.ID, item.LockedAt, "subscriber lookup failed")
		return outboxEnqueueStats{subscriberLookupFailures: 1}
	}

	if len(rooms) == 0 {
		d.markSent(ctx, item.ID, item.LockedAt)
		return outboxEnqueueStats{noSubscriberOutboxes: 1}
	}

	rooms = d.filterLiveCatchupSuppressedRooms(ctx, *item, rooms)
	if len(rooms) == 0 {
		d.markSent(ctx, item.ID, item.LockedAt)
		return outboxEnqueueStats{noSubscriberOutboxes: 1}
	}

	roomIDs := deliveryRoomIDs(rooms)
	if err := d.delivery.EnqueueBatch(ctx, item.ID, roomIDs); err != nil {
		d.logger.Warn("Failed to enqueue room deliveries",
			slog.Int64("outbox_id", item.ID),
			slog.Any("error", err))
		d.markFailed(ctx, item.ID, item.LockedAt, fmt.Sprintf("enqueue delivery rows: %v", err))
		return outboxEnqueueStats{enqueueFailures: 1}
	}

	d.releaseOutboxLock(ctx, item.ID, item.LockedAt)
	return outboxEnqueueStats{enqueuedOutboxes: 1, totalTargetRooms: len(roomIDs)}
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

	outboxEnqueueOutboxesTotal.WithLabelValues("claimed").Add(float64(claimed))
	outboxEnqueueOutboxesTotal.WithLabelValues("enqueued").Add(float64(stats.enqueuedOutboxes))
	outboxEnqueueOutboxesTotal.WithLabelValues("no_subscribers").Add(float64(stats.noSubscriberOutboxes))
	outboxEnqueueOutboxesTotal.WithLabelValues("subscriber_lookup_failures").Add(float64(stats.subscriberLookupFailures))
	outboxEnqueueOutboxesTotal.WithLabelValues("enqueue_failures").Add(float64(stats.enqueueFailures))
	outboxEnqueueTargetRoomsTotal.Add(float64(stats.totalTargetRooms))
}

func (d *ClaimManager) processPendingDeliveries(ctx context.Context) int {
	rows, err := d.delivery.FetchAndLock(ctx, d.config.BatchSize, d.config.LockTimeout)
	if err != nil {
		d.logger.Error("Failed to fetch delivery rows", slog.Any("error", err))
		return 0
	}
	if len(rows) == 0 {
		return 0
	}

	startedAt := time.Now()
	defer func() {
		outboxDispatchDuration.Observe(time.Since(startedAt).Seconds())
	}()

	outboxDeliveryClaimedTotal.Add(float64(len(rows)))
	outboxDispatchBatchSize.Observe(float64(len(rows)))

	outboxByID, err := d.loadOutboxItemsByIDs(ctx, collectDeliveryOutboxIDs(rows))
	if err != nil {
		d.logger.Error("Failed to load outbox rows for deliveries", slog.Any("error", err))
		outboxDeliveryProcessedTotal.WithLabelValues("failed").Add(float64(len(rows)))
		_ = d.delivery.MarkFailedRetryBatchIfLocked(ctx, store.DeliveryLockTokensForIDs(rows, collectDeliveryIDs(rows)), d.config.MaxRetries, d.config.RetryBackoff, "load outbox rows")
		return len(rows)
	}

	result := d.dispatchDeliveryRows(ctx, rows, outboxByID)
	d.markDispatchResult(ctx, rows, result)

	touchedOutboxIDs := deliverysql.UniqueInt64s(result.TouchedOutboxIDs)
	aggregateFailures := d.reconcileTouchedOutboxes(ctx, touchedOutboxIDs)
	d.recordOutboxDispatchResult(len(rows), result, touchedOutboxIDs, aggregateFailures)

	return len(rows)
}

func (d *ClaimManager) markDispatchResult(ctx context.Context, rows []domain.YouTubeNotificationDelivery, result dispatchstate.DispatchResult) {
	if err := d.delivery.MarkSentBatchIfLocked(ctx, store.DeliveryLockTokensForIDs(rows, result.SuccessDeliveryIDs), result.SuccessClaimTokens...); err != nil {
		d.logger.Error("Failed to mark delivery rows as sent", slog.Any("error", err))
		if recoverErr := d.recoverSuccessfulCommunityShortsSentState(ctx, result.SuccessDeliveryIDs); recoverErr != nil {
			d.logger.Warn("Failed to persist community/shorts sent-state recovery after mark-sent error",
				slog.Any("error", recoverErr),
				slog.Int("delivery_count", len(result.SuccessDeliveryIDs)))
		}
	}
	for reason, ids := range result.FailureBuckets {
		d.markFailedDispatchBucket(ctx, rows, reason, ids)
	}
}

func (d *ClaimManager) markFailedDispatchBucket(ctx context.Context, rows []domain.YouTubeNotificationDelivery, reason string, ids []int64) {
	if deliveryFailureReasonIsPermanent(reason) {
		d.markPermanentDispatchFailureBucket(ctx, rows, reason, ids)
		return
	}
	d.markRetryDispatchFailureBucket(ctx, rows, reason, ids)
}

func (d *ClaimManager) markPermanentDispatchFailureBucket(ctx context.Context, rows []domain.YouTubeNotificationDelivery, reason string, ids []int64) {
	if err := d.delivery.MarkPermanentFailureBatchIfLocked(ctx, store.DeliveryLockTokensForIDs(rows, ids), d.config.MaxRetries, reason); err != nil {
		d.logger.Error("Failed to mark delivery rows as permanent failed",
			slog.String("reason", reason),
			slog.Any("error", err))
	}
}

func (d *ClaimManager) markRetryDispatchFailureBucket(ctx context.Context, rows []domain.YouTubeNotificationDelivery, reason string, ids []int64) {
	if err := d.delivery.MarkFailedRetryBatchIfLocked(ctx, store.DeliveryLockTokensForIDs(rows, ids), d.config.MaxRetries, d.config.RetryBackoff, reason); err != nil {
		d.logger.Error("Failed to mark delivery rows as failed",
			slog.String("reason", reason),
			slog.Any("error", err))
	}
}

func (d *ClaimManager) reconcileTouchedOutboxes(ctx context.Context, touchedOutboxIDs []int64) int {
	if err := d.delivery.UpdateOutboxAggregateStatuses(ctx, touchedOutboxIDs); err != nil {
		d.logger.Warn("Failed to update outbox aggregate statuses", slog.Any("error", err))
		return 1
	} else if err := d.logFinalizedCommunityShortsOutboxResults(ctx, touchedOutboxIDs); err != nil {
		d.logger.Warn("Failed to log finalized community/shorts outbox results", slog.Any("error", err))
	}

	return 0
}

func (d *ClaimManager) recordOutboxDispatchResult(
	claimed int,
	result dispatchstate.DispatchResult,
	touchedOutboxIDs []int64,
	aggregateFailures int,
) {
	outboxDeliveryProcessedTotal.WithLabelValues("sent").Add(float64(len(result.SuccessDeliveryIDs)))
	outboxDeliveryProcessedTotal.WithLabelValues("failed").Add(float64(result.FailedDeliveries))
	outboxDispatchTouchedOutboxes.Observe(float64(len(touchedOutboxIDs)))

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
	if err := deliverysql.SelectDeliverySQL(ctx, d.db, &rows, "load outbox rows by ids", `
			SELECT id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error
		FROM youtube_notification_outbox
		WHERE `+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
	`, deliverysql.AppendDeliveryInt64Args(nil, uniqueIDs)...); err != nil {
		return nil, fmt.Errorf("load outbox rows by ids: %w", err)
	}

	result := make(map[int64]domain.YouTubeNotificationOutbox, len(rows))
	for i := range rows {
		result[rows[i].ID] = rows[i]
	}
	return result, nil
}
