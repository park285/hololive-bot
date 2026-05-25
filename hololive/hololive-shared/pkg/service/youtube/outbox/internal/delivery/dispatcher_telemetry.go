package delivery

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *Dispatcher) telemetryLoop(ctx context.Context) {
	d.telemetry.telemetryLoop(ctx)
}

func (d *Dispatcher) processDeliveryTelemetry(ctx context.Context) {
	d.telemetry.processDeliveryTelemetry(ctx)
}

func buildDeliveryAuditLogAttrs(row domain.YouTubeNotificationDeliveryTelemetry) []any {
	return buildDeliveryAuditLogAttrsWithClassification(row, PostLatencyClassificationResult{})
}
