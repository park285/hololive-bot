package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
)

func (d *ClaimManager) logFinalizedCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) error {
	if err := d.auditLogger.logFinalizedCommunityShortsOutboxResults(ctx, outboxIDs); err != nil {
		return fmt.Errorf("log finalized community shorts outbox results: %w", err)
	}

	return nil
}

func appendLatencyClassificationLogAttr(attrs []any, classification *timeline.PostLatencyClassificationResult) []any {
	if classification == nil || classification.Status == "" {
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
