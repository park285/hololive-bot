package delivery

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *Dispatcher) telemetryLoop(ctx context.Context) {
	d.telemetry.telemetryLoop(ctx)
}

func (d *Dispatcher) processDeliveryTelemetry(ctx context.Context) {
	d.telemetry.processDeliveryTelemetry(ctx)
}

func (d *Dispatcher) backfillDeliveryTelemetry(ctx context.Context) {
	d.telemetry.backfillDeliveryTelemetry(ctx)
}

func (d *Dispatcher) deliveryTelemetryBackfillSince() time.Time {
	return d.telemetry.deliveryTelemetryBackfillSince()
}

func (d *Dispatcher) fetchDeliveryTelemetryRows(ctx context.Context) ([]domain.YouTubeNotificationDeliveryTelemetry, bool) {
	return d.telemetry.fetchDeliveryTelemetryRows(ctx)
}

func (d *Dispatcher) loadDeliveryTelemetryClassificationsForRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) map[int64]PostLatencyClassificationResult {
	return d.telemetry.loadDeliveryTelemetryClassificationsForRows(ctx, rows)
}

func (d *Dispatcher) emitDeliveryTelemetryRows(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	classificationsByOutboxID map[int64]PostLatencyClassificationResult,
) ([]int64, []int64) {
	return d.telemetry.emitDeliveryTelemetryRows(rows, classificationsByOutboxID)
}

func (d *Dispatcher) markDeliveryTelemetryResults(ctx context.Context, loggedIDs, failedIDs []int64) {
	d.telemetry.markDeliveryTelemetryResults(ctx, loggedIDs, failedIDs)
}

func buildDeliveryAuditLogAttrs(row domain.YouTubeNotificationDeliveryTelemetry) []any {
	return buildDeliveryAuditLogAttrsWithClassification(row, PostLatencyClassificationResult{})
}
