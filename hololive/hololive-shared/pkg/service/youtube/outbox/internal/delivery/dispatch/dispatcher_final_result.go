package dispatch

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

func (d *ClaimManager) logFinalizedCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) error {
	return d.auditLogger.logFinalizedCommunityShortsOutboxResults(ctx, outboxIDs)
}

func appendLatencyClassificationLogAttr(attrs []any, classification PostLatencyClassificationResult) []any {
	if classification.Status == "" {
		return attrs
	}

	return append(attrs, slog.Group(logschema.FieldLatencyClassification,
		slog.String("status", string(classification.Status)),
		slog.Int64("threshold_millis", classification.ThresholdMillis),
		slog.String("delay_source", string(classification.DelaySource)),
		slog.String("internal_delay_cause", string(classification.InternalDelayCause)),
		slog.String("reason_code", string(timeline.ClassifyPostLatencyReasonCode(classification))),
		slog.Any("evidence", classification.Evidence),
	))
}
