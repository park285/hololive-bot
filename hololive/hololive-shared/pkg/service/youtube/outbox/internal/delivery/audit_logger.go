package delivery

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/telemetry"
)

type AuditLogger struct {
	telemetry          *DeliveryTelemetryRepository
	delivery           *DeliveryRepository
	logger             *slog.Logger
	config             Config
	telemetryProcessor *TelemetryProcessor
}

func newAuditLogger(
	telemetry *DeliveryTelemetryRepository,
	deliveryRepo *DeliveryRepository,
	logger *slog.Logger,
	config Config,
	telemetryProcessor *TelemetryProcessor,
) *AuditLogger {
	return &AuditLogger{
		telemetry:          telemetry,
		delivery:           deliveryRepo,
		logger:             logger,
		config:             config,
		telemetryProcessor: telemetryProcessor,
	}
}

func (al *AuditLogger) logCommunityShortsDeliveryAttemptStarted(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	attemptStartedAt time.Time,
	deliveryMode string,
) {
	limit := min(len(outboxes), len(rows))
	if limit == 0 {
		return
	}

	attemptStartedAt = attemptStartedAt.UTC()
	deliveryPath := telemetry.NormalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
	limitedRows := rows[:limit]
	limitedOutboxes := outboxes[:limit]

	for i := range limitedOutboxes {
		outbox := limitedOutboxes[i]
		if !telemetry.IsCommunityShortsDeliveryAuditKind(outbox.Kind) {
			continue
		}

		al.logger.Info(deliveryAttemptStartedLogMessage,
			slog.Int64(logschema.FieldDeliveryID, limitedRows[i].ID),
			slog.Int64(logschema.FieldOutboxID, outbox.ID),
			slog.String(logschema.FieldRoomID, limitedRows[i].RoomID),
			slog.String(logschema.FieldChannelID, outbox.ChannelID),
			slog.String(deliveryAuditPostIDLogField, telemetry.ResolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload)),
			slog.String(deliveryAuditContentIDLogField, strings.TrimSpace(outbox.ContentID)),
			slog.String(deliveryAuditAlarmTypeLogField, string(outbox.Kind.ToAlarmType())),
			slog.Time(deliveryAttemptStartedAtLogField, attemptStartedAt),
			slog.Int(logschema.FieldAttemptOrdinal, deliveryAttemptOrdinal(limitedRows[i])),
			slog.String(deliveryAuditPathLogField, deliveryPath),
			slog.String(deliveryAuditModeLogField, deliveryMode),
			slog.String(deliveryDedupeKeyLogField, telemetry.DedupeKeyLogValue(outbox)),
		)
	}
}

func (al *AuditLogger) logCommunityShortsDeliveryResult(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryMode string,
	sendResult string,
	failureReason string,
) {
	if al == nil || al.logger == nil {
		return
	}

	limit := min(len(outboxes), len(rows))
	if limit == 0 {
		return
	}

	sentAt = sentAt.UTC()
	deliveryPath := telemetry.NormalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
	summary := summarizeCommunityShortsDeliveryResult(rows[:limit], outboxes[:limit])
	if summary.alarmCount == 0 {
		return
	}

	roomCount := len(summary.uniqueRooms)
	successfulAlarmCount, failedAlarmCount, successfulRoomCount, failedRoomCount := deliveryResultCounts(sendResult, summary.alarmCount, roomCount)

	attrs := []any{
		slog.String(logschema.FieldChannelID, summary.channelID),
		slog.String(deliveryAuditAlarmTypeLogField, string(summary.alarmType)),
		slog.Time(deliveryAuditSentAtLogField, sentAt),
		slog.String(deliveryAuditSendResultLogField, sendResult),
		slog.String(deliveryAuditPathLogField, deliveryPath),
		slog.String(deliveryAuditModeLogField, deliveryMode),
		slog.Int(logschema.FieldTargetAlarmCount, summary.alarmCount),
		slog.Int(logschema.FieldSuccessfulAlarmCount, successfulAlarmCount),
		slog.Int(logschema.FieldFailedAlarmCount, failedAlarmCount),
		slog.Int(logschema.FieldTargetRoomCount, roomCount),
		slog.Int(logschema.FieldSuccessfulRoomCount, successfulRoomCount),
		slog.Int(logschema.FieldFailedRoomCount, failedRoomCount),
	}
	if roomCount == 1 {
		for roomID := range summary.uniqueRooms {
			attrs = append(attrs, slog.String(logschema.FieldRoomID, roomID))
			break
		}
	}
	if trimmedReason := strings.TrimSpace(failureReason); trimmedReason != "" {
		attrs = append(attrs, slog.String(deliveryAuditFailureReasonLogField, deliverysql.TruncateString(trimmedReason, 100)))
	}

	al.logger.Info(deliveryResultLogMessage, attrs...)
}

func (al *AuditLogger) logCommunityShortsDeliveryAudit(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryMode string,
	sendResult string,
	failureReason string,
	sendErr error,
) {
	limit := min(len(outboxes), len(rows))
	if limit == 0 {
		return
	}

	sentAt = sentAt.UTC()
	deliveryPath := telemetry.NormalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
	events := buildCommunityShortsDeliveryAuditEvents(
		rows[:limit],
		outboxes[:limit],
		sentAt,
		deliveryPath,
		deliveryMode,
		sendResult,
		failureReason,
	)
	if len(events) == 0 {
		return
	}

	preparedEvents, telemetryAvailable := al.prepareCommunityShortsDeliveryAuditEvents(ctx, events)
	if telemetryAvailable && al.enqueueCommunityShortsDeliveryAuditEvents(ctx, preparedEvents) {
		return
	}

	al.logCommunityShortsDeliveryAuditFallback(ctx, preparedEvents, sendErr)
}

func (al *AuditLogger) prepareCommunityShortsDeliveryAuditEvents(
	ctx context.Context,
	events []domain.YouTubeNotificationDeliveryTelemetry,
) ([]domain.YouTubeNotificationDeliveryTelemetry, bool) {
	if al.telemetry == nil {
		return events, false
	}

	prepared, err := al.telemetry.PrepareRows(ctx, events)
	if err != nil {
		al.logger.Warn("Failed to enrich persistent delivery audit",
			slog.Int("events", len(events)),
			slog.Any("error", err))
		return events, false
	}
	return prepared, true
}

func (al *AuditLogger) enqueueCommunityShortsDeliveryAuditEvents(
	ctx context.Context,
	preparedEvents []domain.YouTubeNotificationDeliveryTelemetry,
) bool {
	enqueueErr := al.telemetry.EnqueuePrepared(ctx, preparedEvents)
	if enqueueErr != nil {
		al.logger.Warn("Failed to enqueue persistent delivery audit",
			slog.Int("events", len(preparedEvents)),
			slog.Any("error", enqueueErr))
		return false
	}

	if err := al.telemetry.PersistPostLatencyClassificationsByOutboxIDs(ctx, telemetry.CollectTelemetryOutboxIDs(preparedEvents)); err != nil {
		al.logger.Warn("Failed to persist post latency classifications",
			slog.Int("events", len(preparedEvents)),
			slog.Any("error", err))
	}
	return true
}

func (al *AuditLogger) logCommunityShortsDeliveryAuditFallback(
	ctx context.Context,
	preparedEvents []domain.YouTubeNotificationDeliveryTelemetry,
	sendErr error,
) {
	fallbackClassificationsByOutboxID, err := al.telemetryProcessor.loadDeliveryTelemetryLatencyClassifications(ctx, preparedEvents)
	if err != nil {
		al.logger.Warn("Failed to load fallback delivery telemetry latency classifications",
			slog.Int("events", len(preparedEvents)),
			slog.Any("error", err))
	}

	for i := range preparedEvents {
		attrs := buildDeliveryAuditLogAttrsWithClassification(preparedEvents[i], fallbackClassificationsByOutboxID[preparedEvents[i].OutboxID])
		attrs = append(attrs, slog.String(logschema.FieldTelemetrySource, "direct_fallback"))
		if sendErr != nil {
			attrs = append(attrs, slog.String("error", sendErr.Error()))
		}

		al.logger.Info(deliveryAuditLogMessage, attrs...)
	}
}

func (al *AuditLogger) logFinalizedCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) error {
	if al == nil || al.delivery == nil || al.logger == nil {
		return nil
	}

	results, err := al.delivery.LoadTerminalCommunityShortsOutboxResults(ctx, outboxIDs)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}

	timelinesByOutboxID, err := al.loadFinalizedCommunityShortsTimelines(ctx, outboxIDs, len(results))
	if err != nil {
		return err
	}

	finalizedAt := time.Now().UTC()
	for i := range results {
		al.logFinalizedCommunityShortsOutboxResultWithTimeline(results[i], timelinesByOutboxID, finalizedAt)
	}

	return nil
}

func (al *AuditLogger) loadFinalizedCommunityShortsTimelines(
	ctx context.Context,
	outboxIDs []int64,
	resultCount int,
) (map[int64]PostDeliveryTimeline, error) {
	timelinesByOutboxID := make(map[int64]PostDeliveryTimeline, resultCount)
	if al.telemetry == nil {
		return timelinesByOutboxID, nil
	}

	timelines, err := al.telemetry.ListPostDeliveryTimelinesByOutboxIDs(ctx, outboxIDs)
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

func (al *AuditLogger) logFinalizedCommunityShortsOutboxResultWithTimeline(
	result terminalCommunityShortsOutboxResult,
	timelinesByOutboxID map[int64]PostDeliveryTimeline,
	finalizedAt time.Time,
) {
	timing := alarmtiming.Build(nil, result.SentAt)
	if timeline, ok := timelinesByOutboxID[result.OutboxID]; ok {
		result.LatencyClassification = timeline.LatencyClassification
		timing = communityShortsAlarmTimingForTimeline(timeline)
	}
	al.logFinalizedCommunityShortsOutboxResult(result, finalizedAt, timing)
}

func (al *AuditLogger) logFinalizedCommunityShortsOutboxResult(
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
		slog.String(deliveryAuditPostIDLogField, telemetry.ResolveTelemetryPostID(result.Kind, result.ContentID, result.Payload)),
		slog.String(deliveryAuditContentIDLogField, result.ContentID),
		slog.String(deliveryAuditAlarmTypeLogField, string(result.Kind.ToAlarmType())),
		slog.Time(deliveryAuditSentAtLogField, eventAt),
		slog.String(deliveryAuditSendResultLogField, sendResult),
		slog.String(deliveryAuditPathLogField, telemetry.NormalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)),
		slog.String(deliveryAuditModeLogField, logschema.DeliveryModeFinalResult),
		slog.String(deliveryDedupeKeyLogField, telemetry.DedupeKeyLogValue(outbox)),
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

	al.logger.Info(deliveryAuditLogMessage, attrs...)
}
