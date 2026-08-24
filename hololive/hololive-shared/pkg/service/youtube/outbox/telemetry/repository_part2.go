package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

func (r *Repository) refreshLockedRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) error {
	if len(rows) == 0 {
		return nil
	}

	enriched := append([]domain.YouTubeNotificationDeliveryTelemetry(nil), rows...)
	if err := r.enrichRows(ctx, enriched); err != nil {
		return fmt.Errorf("refresh locked delivery telemetry rows: %w", err)
	}

	ids := make([]int64, 0, len(enriched))
	actualPublishedAt := make([]*time.Time, 0, len(enriched))
	alarmSentAt := make([]*time.Time, 0, len(enriched))
	alarmLatencyMillis := make([]*int64, 0, len(enriched))
	detectedAt := make([]*time.Time, 0, len(enriched))

	for i := range enriched {
		if deliveryTelemetryTrackingContextChanged(&rows[i], &enriched[i]) {
			ids = append(ids, enriched[i].ID)
			actualPublishedAt = append(actualPublishedAt, enriched[i].ActualPublishedAt)
			alarmSentAt = append(alarmSentAt, enriched[i].AlarmSentAt)
			alarmLatencyMillis = append(alarmLatencyMillis, enriched[i].AlarmLatencyMillis)
			detectedAt = append(detectedAt, enriched[i].DetectedAt)
		}

		rows[i] = enriched[i]
	}

	if len(ids) == 0 {
		return nil
	}

	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "refresh locked delivery telemetry rows", mustSQL("repository_0343_08.sql"), ids, actualPublishedAt, alarmSentAt, alarmLatencyMillis, detectedAt); err != nil {
		return fmt.Errorf("exec delivery SQL: %w", err)
	}

	return nil
}

const retentionDeleteBatchSize = 1000

func (r *Repository) DeleteLoggedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	out, err := r.deleteLoggedBeforeInBatches(ctx, cutoff, retentionDeleteBatchSize)
	if err != nil {
		return out, fmt.Errorf("delete logged before in batches: %w", err)
	}

	return out, nil
}

func (r *Repository) deleteLoggedBeforeInBatches(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if r == nil || r.db == nil || cutoff.IsZero() {
		return 0, nil
	}

	var total int64

	for {
		deleted, done, err := r.deleteLoggedBeforeBatch(ctx, cutoff, batchSize)

		total += deleted

		if err != nil {
			return total, fmt.Errorf("delete logged before batch: %w", err)
		}

		if done {
			return total, nil
		}
	}
}

func (r *Repository) deleteLoggedBeforeBatch(ctx context.Context, cutoff time.Time, batchSize int) (deleted int64, done bool, err error) {
	tag, err := r.db.Exec(ctx, mustSQL("repository_0364_09.sql"), cutoff.UTC(), batchSize)
	if err != nil {
		return 0, true, fmt.Errorf("delete delivery telemetry before cutoff: %w", err)
	}

	deleted = tag.RowsAffected()
	if deleted < int64(batchSize) {
		return deleted, true, nil
	}

	if err := deliverysql.YieldBetweenDeleteBatches(ctx); err != nil {
		return deleted, true, fmt.Errorf("yield between delete batches: %w", err)
	}

	return deleted, false, nil
}

func CollectTelemetryOutboxIDs(rows []domain.YouTubeNotificationDeliveryTelemetry) []int64 {
	outboxIDs := make([]int64, 0, len(rows))
	for i := range rows {
		if rows[i].OutboxID <= 0 {
			continue
		}

		outboxIDs = append(outboxIDs, rows[i].OutboxID)
	}

	return deliverysql.UniqueInt64s(outboxIDs)
}
