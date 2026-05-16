package delivery

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
)

func (d *Dispatcher) logFinalizedCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) error {
	if d == nil || d.delivery == nil || d.logger == nil {
		return nil
	}

	results, err := d.delivery.LoadTerminalCommunityShortsOutboxResults(ctx, outboxIDs)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}

	timelinesByOutboxID, err := d.loadFinalizedCommunityShortsTimelines(ctx, outboxIDs, len(results))
	if err != nil {
		return err
	}

	finalizedAt := time.Now().UTC()
	for i := range results {
		d.logFinalizedCommunityShortsOutboxResultWithTimeline(results[i], timelinesByOutboxID, finalizedAt)
	}

	return nil
}

func (d *Dispatcher) loadFinalizedCommunityShortsTimelines(
	ctx context.Context,
	outboxIDs []int64,
	resultCount int,
) (map[int64]PostDeliveryTimeline, error) {
	timelinesByOutboxID := make(map[int64]PostDeliveryTimeline, resultCount)
	if d.telemetry == nil {
		return timelinesByOutboxID, nil
	}

	timelines, err := d.telemetry.ListPostDeliveryTimelinesByOutboxIDs(ctx, outboxIDs)
	if err != nil {
		return nil, err
	}
	for i := range timelines {
		if timelines[i].OutboxID == 0 {
			continue
		}
		timelinesByOutboxID[timelines[i].OutboxID] = timelines[i]
	}
	return timelinesByOutboxID, nil
}

func (d *Dispatcher) logFinalizedCommunityShortsOutboxResultWithTimeline(
	result terminalCommunityShortsOutboxResult,
	timelinesByOutboxID map[int64]PostDeliveryTimeline,
	finalizedAt time.Time,
) {
	timing := alarmtiming.Build(nil, result.SentAt)
	if timeline, ok := timelinesByOutboxID[result.OutboxID]; ok {
		result.LatencyClassification = timeline.LatencyClassification
		timing = communityShortsAlarmTimingForTimeline(timeline)
	}
	d.logFinalizedCommunityShortsOutboxResult(result, finalizedAt, timing)
}

func (d *Dispatcher) logFinalizedCommunityShortsOutboxResult(
	result terminalCommunityShortsOutboxResult,
	finalizedAt time.Time,
	timing alarmtiming.Snapshot,
) {
	sendResult := "failure"
	eventAt := finalizedAt
	if result.Status == domain.OutboxStatusSent {
		sendResult = "success"
		if result.SentAt != nil && !result.SentAt.IsZero() {
			eventAt = result.SentAt.UTC()
		}
	}

	outbox := domain.YouTubeNotificationOutbox{
		ID:        result.OutboxID,
		Kind:      result.Kind,
		ChannelID: result.ChannelID,
		ContentID: result.ContentID,
		Payload:   result.Payload,
	}

	attrs := []any{
		slog.Int64(logschema.FieldOutboxID, result.OutboxID),
		slog.String(logschema.FieldChannelID, result.ChannelID),
		slog.String(deliveryAuditPostIDLogField, resolveTelemetryPostID(result.Kind, result.ContentID, result.Payload)),
		slog.String(deliveryAuditContentIDLogField, result.ContentID),
		slog.String(deliveryAuditAlarmTypeLogField, string(result.Kind.ToAlarmType())),
		slog.Time(deliveryAuditSentAtLogField, eventAt),
		slog.String(deliveryAuditSendResultLogField, sendResult),
		slog.String(deliveryAuditPathLogField, normalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)),
		slog.String(deliveryAuditModeLogField, logschema.DeliveryModeFinalResult),
		slog.String(deliveryDedupeKeyLogField, dedupeKeyLogValue(outbox)),
		slog.String(logschema.FieldTelemetrySource, logschema.TelemetrySourceOutboxFinalResult),
		slog.Int(logschema.FieldTargetRoomCount, result.TargetRoomCount),
		slog.Int(logschema.FieldSuccessfulRoomCount, result.SuccessfulRoomCount),
		slog.Int(logschema.FieldFailedRoomCount, result.FailedRoomCount),
	}
	attrs = appendCommunityShortsAlarmTimingLogAttrs(attrs, timing)
	if result.AggregatedFailReason != "" {
		attrs = append(attrs, slog.String(deliveryAuditFailureReasonLogField, result.AggregatedFailReason))
	}
	attrs = appendLatencyClassificationLogAttr(attrs, result.LatencyClassification)

	d.logger.Info(deliveryAuditLogMessage, attrs...)
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
		slog.String("reason_code", string(classifyPostLatencyReasonCode(classification))),
		slog.Any("evidence", classification.Evidence),
	))
}
