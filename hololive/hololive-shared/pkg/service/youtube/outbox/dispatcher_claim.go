package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *Dispatcher) claimOutboxBatch(ctx context.Context) ([]domain.YouTubeNotificationOutbox, error) {
	return d.fetchAndLockForPerRoom(ctx)
}

// fetchAndLockForPerRoom: delivery row가 아직 없는 outbox만 claim
func (d *Dispatcher) fetchAndLockForPerRoom(ctx context.Context) ([]domain.YouTubeNotificationOutbox, error) {
	var items []domain.YouTubeNotificationOutbox
	now := time.Now()
	lockExpiry := now.Add(-d.cfg.LockTimeout)

	if err := d.db.WithContext(ctx).Raw(`
		WITH claim AS (
			SELECT o.id
			FROM youtube_notification_outbox o
			WHERE o.status = ?
			  AND (o.locked_at IS NULL OR o.locked_at < ?)
			  AND o.next_attempt_at <= ?
			  AND NOT EXISTS (
				SELECT 1 FROM youtube_notification_delivery d
				WHERE d.outbox_id = o.id
			  )
			ORDER BY o.created_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		),
		updated AS (
			UPDATE youtube_notification_outbox o
			SET locked_at = ?
			FROM claim
			WHERE o.id = claim.id
			RETURNING o.id, o.kind, o.channel_id, o.content_id, o.payload, o.status,
			          o.attempt_count, o.next_attempt_at, o.created_at, o.locked_at, o.sent_at, o.error
		)
		SELECT id, kind, channel_id, content_id, payload, status,
		       attempt_count, next_attempt_at, created_at, locked_at, sent_at, error
		FROM updated
		ORDER BY created_at ASC
	`, domain.OutboxStatusPending, lockExpiry, now, d.cfg.BatchSize, now).Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("fetch and lock outbox items for per-room mode: %w", err)
	}

	return items, nil
}

func (d *Dispatcher) processPerRoomBatch(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox) {
	roomsByChannel := d.collectRoomsByChannel(ctx, outboxItems)
	d.enqueueDeliveries(ctx, outboxItems, roomsByChannel)
	d.processPendingDeliveries(ctx)
}

func (d *Dispatcher) enqueueDeliveries(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox, roomsByChannel map[string]map[string]bool) {
	startedAt := time.Now()
	defer func() {
		outboxEnqueueDuration.Observe(time.Since(startedAt).Seconds())
	}()

	enqueuedOutboxes := 0
	noSubscriberOutboxes := 0
	subscriberLookupFailures := 0
	enqueueFailures := 0
	totalTargetRooms := 0

	for i := range outboxItems {
		item := &outboxItems[i]
		rooms, ok := roomsByChannel[item.ChannelID]
		if !ok {
			subscriberLookupFailures++
			d.releaseOutboxLock(ctx, item.ID)
			continue
		}

		if len(rooms) == 0 {
			noSubscriberOutboxes++
			d.markSent(ctx, item.ID)
			continue
		}

		roomIDs := make([]string, 0, len(rooms))
		for roomID := range rooms {
			roomIDs = append(roomIDs, roomID)
		}

		if err := d.delivery.EnqueueBatch(ctx, item.ID, roomIDs); err != nil {
			enqueueFailures++
			d.logger.Warn("Failed to enqueue room deliveries",
				slog.Int64("outbox_id", item.ID),
				slog.Any("error", err))
			d.markFailed(ctx, item.ID, fmt.Sprintf("enqueue delivery rows: %v", err))
			continue
		}

		enqueuedOutboxes++
		totalTargetRooms += len(roomIDs)
		d.releaseOutboxLock(ctx, item.ID)
	}

	d.logger.Info("Outbox per-room enqueue completed",
		slog.Int("outbox_claimed", len(outboxItems)),
		slog.Int("outbox_enqueued", enqueuedOutboxes),
		slog.Int("outbox_no_subscribers", noSubscriberOutboxes),
		slog.Int("subscriber_lookup_failures", subscriberLookupFailures),
		slog.Int("enqueue_failures", enqueueFailures),
		slog.Int("target_rooms", totalTargetRooms))

	outboxEnqueueOutboxesTotal.WithLabelValues("claimed").Add(float64(len(outboxItems)))
	outboxEnqueueOutboxesTotal.WithLabelValues("enqueued").Add(float64(enqueuedOutboxes))
	outboxEnqueueOutboxesTotal.WithLabelValues("no_subscribers").Add(float64(noSubscriberOutboxes))
	outboxEnqueueOutboxesTotal.WithLabelValues("subscriber_lookup_failures").Add(float64(subscriberLookupFailures))
	outboxEnqueueOutboxesTotal.WithLabelValues("enqueue_failures").Add(float64(enqueueFailures))
	outboxEnqueueTargetRoomsTotal.Add(float64(totalTargetRooms))
}

func (d *Dispatcher) processPendingDeliveries(ctx context.Context) {
	rows, err := d.delivery.FetchAndLock(ctx, d.cfg.BatchSize, d.cfg.LockTimeout)
	if err != nil {
		d.logger.Error("Failed to fetch delivery rows", slog.Any("error", err))
		return
	}
	if len(rows) == 0 {
		return
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
		_ = d.delivery.MarkFailedRetryBatch(ctx, collectDeliveryIDs(rows), d.cfg.MaxRetries, d.cfg.RetryBackoff, "load outbox rows")
		return
	}

	result := d.dispatchDeliveryRows(ctx, rows, outboxByID)

	if err := d.delivery.MarkSentBatch(ctx, result.successDeliveryIDs); err != nil {
		d.logger.Error("Failed to mark delivery rows as sent", slog.Any("error", err))
	}
	for reason, ids := range result.failureBuckets {
		if err := d.delivery.MarkFailedRetryBatch(ctx, ids, d.cfg.MaxRetries, d.cfg.RetryBackoff, reason); err != nil {
			d.logger.Error("Failed to mark delivery rows as failed",
				slog.String("reason", reason),
				slog.Any("error", err))
		}
	}

	touchedOutboxIDs := uniqueInt64s(result.touchedOutboxIDs)
	aggregateFailures := 0
	if err := d.delivery.UpdateOutboxAggregateStatuses(ctx, touchedOutboxIDs); err != nil {
		aggregateFailures++
		d.logger.Warn("Failed to update outbox aggregate statuses", slog.Any("error", err))
	}

	outboxDeliveryProcessedTotal.WithLabelValues("sent").Add(float64(len(result.successDeliveryIDs)))
	outboxDeliveryProcessedTotal.WithLabelValues("failed").Add(float64(result.failedDeliveries))
	outboxDispatchTouchedOutboxes.Observe(float64(len(touchedOutboxIDs)))

	d.logger.Info("Outbox per-room dispatch completed",
		slog.Int("delivery_claimed", len(rows)),
		slog.Int("delivery_sent", len(result.successDeliveryIDs)),
		slog.Int("delivery_failed", result.failedDeliveries),
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

func (d *Dispatcher) loadOutboxItemsByIDs(ctx context.Context, ids []int64) (map[int64]domain.YouTubeNotificationOutbox, error) {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return map[int64]domain.YouTubeNotificationOutbox{}, nil
	}

	var rows []domain.YouTubeNotificationOutbox
	if err := d.db.WithContext(ctx).
		Where("id IN ?", uniqueIDs).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load outbox rows by ids: %w", err)
	}

	result := make(map[int64]domain.YouTubeNotificationOutbox, len(rows))
	for i := range rows {
		result[rows[i].ID] = rows[i]
	}
	return result, nil
}

func (d *Dispatcher) releaseOutboxLock(ctx context.Context, id int64) {
	result := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ? AND status = ?", id, domain.OutboxStatusPending).
		Update("locked_at", nil)
	if result.Error != nil {
		d.logger.Warn("Failed to release outbox lock",
			slog.Int64("id", id),
			slog.Any("error", result.Error))
	}
}
