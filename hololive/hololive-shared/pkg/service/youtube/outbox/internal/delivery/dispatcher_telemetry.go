package delivery

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *Dispatcher) telemetryLoop(ctx context.Context) {
	d.telemetryProcessor.telemetryLoop(ctx)
}

func (d *Dispatcher) processDeliveryTelemetry(ctx context.Context) {
	d.telemetryProcessor.processDeliveryTelemetry(ctx)
}

func (d *Dispatcher) backfillDeliveryTelemetry(ctx context.Context) {
	d.telemetryProcessor.backfillDeliveryTelemetry(ctx)
}

func (d *Dispatcher) deliveryTelemetryBackfillSince() time.Time {
	return d.telemetryProcessor.deliveryTelemetryBackfillSince()
}

func (d *Dispatcher) fetchDeliveryTelemetryRows(ctx context.Context) ([]domain.YouTubeNotificationDeliveryTelemetry, bool) {
	return d.telemetryProcessor.fetchDeliveryTelemetryRows(ctx)
}

func (d *Dispatcher) loadDeliveryTelemetryClassificationsForRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) map[int64]PostLatencyClassificationResult {
	return d.telemetryProcessor.loadDeliveryTelemetryClassificationsForRows(ctx, rows)
}

func (d *Dispatcher) emitDeliveryTelemetryRows(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	classificationsByOutboxID map[int64]PostLatencyClassificationResult,
) ([]int64, []int64) {
	return d.telemetryProcessor.emitDeliveryTelemetryRows(rows, classificationsByOutboxID)
}

func (d *Dispatcher) markDeliveryTelemetryResults(ctx context.Context, loggedIDs, failedIDs []int64) {
	d.telemetryProcessor.markDeliveryTelemetryResults(ctx, loggedIDs, failedIDs)
}

func buildDeliveryAuditLogAttrs(row domain.YouTubeNotificationDeliveryTelemetry) []any {
	return buildDeliveryAuditLogAttrsWithClassification(row, PostLatencyClassificationResult{})
}
