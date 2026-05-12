package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
)

func dedupeKeyLogValue(outbox domain.YouTubeNotificationOutbox) string {
	dedupeKey, err := outbox.DedupeKey()
	if err == nil {
		return dedupeKey
	}

	return fmt.Sprintf("invalid:%s:%s",
		strings.TrimSpace(string(outbox.Kind)),
		strings.TrimSpace(outbox.ContentID),
	)
}

func dedupeKeyLogAttrForOutboxes(outboxes []domain.YouTubeNotificationOutbox) slog.Attr {
	dedupeKeys := make([]string, 0, len(outboxes))
	for i := range outboxes {
		dedupeKeys = append(dedupeKeys, dedupeKeyLogValue(outboxes[i]))
	}
	return dedupeKeyLogAttr(dedupeKeys)
}

func normalizeCommunityShortsDeliveryPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return communityShortsDeliveryPath
	}
	return trimmed
}

func deliveryAttemptOrdinal(row domain.YouTubeNotificationDelivery) int {
	attemptOrdinal := row.AttemptCount + 1
	if attemptOrdinal <= 0 {
		return 1
	}
	return attemptOrdinal
}

func deliveryAttemptStartedAt(row domain.YouTubeNotificationDelivery) *time.Time {
	if row.LockedAt == nil || row.LockedAt.IsZero() {
		return nil
	}

	startedAt := row.LockedAt.UTC()
	return &startedAt
}

func isCommunityShortsDeliveryAuditKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindNewShort, domain.OutboxKindCommunityPost:
		return true
	default:
		return false
	}
}

func (d *Dispatcher) logCommunityShortsDeliveryAttemptStarted(
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
	deliveryPath := normalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
	limitedRows := rows[:limit]
	limitedOutboxes := outboxes[:limit]

	for i := range limitedOutboxes {
		outbox := limitedOutboxes[i]
		if !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
			continue
		}

		d.logger.Info(deliveryAttemptStartedLogMessage,
			slog.Int64(logschema.FieldDeliveryID, limitedRows[i].ID),
			slog.Int64(logschema.FieldOutboxID, outbox.ID),
			slog.String(logschema.FieldRoomID, limitedRows[i].RoomID),
			slog.String(logschema.FieldChannelID, outbox.ChannelID),
			slog.String(deliveryAuditPostIDLogField, resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload)),
			slog.String(deliveryAuditContentIDLogField, strings.TrimSpace(outbox.ContentID)),
			slog.String(deliveryAuditAlarmTypeLogField, string(outbox.Kind.ToAlarmType())),
			slog.Time(deliveryAttemptStartedAtLogField, attemptStartedAt),
			slog.Int(logschema.FieldAttemptOrdinal, deliveryAttemptOrdinal(limitedRows[i])),
			slog.String(deliveryAuditPathLogField, deliveryPath),
			slog.String(deliveryAuditModeLogField, deliveryMode),
			slog.String(deliveryDedupeKeyLogField, dedupeKeyLogValue(outbox)),
		)
	}
}

func (d *Dispatcher) logCommunityShortsDeliveryResult(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryMode string,
	sendResult string,
	failureReason string,
) {
	if d == nil || d.logger == nil {
		return
	}

	limit := min(len(outboxes), len(rows))
	if limit == 0 {
		return
	}

	sentAt = sentAt.UTC()
	deliveryPath := normalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
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
		attrs = append(attrs, slog.String(deliveryAuditFailureReasonLogField, truncateString(trimmedReason, 100)))
	}

	d.logger.Info(deliveryResultLogMessage, attrs...)
}

type communityShortsDeliveryResultSummary struct {
	alarmCount  int
	channelID   string
	alarmType   domain.AlarmType
	uniqueRooms map[string]struct{}
}

func summarizeCommunityShortsDeliveryResult(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
) communityShortsDeliveryResultSummary {
	summary := communityShortsDeliveryResultSummary{
		uniqueRooms: make(map[string]struct{}, len(rows)),
	}

	for i := range outboxes {
		outbox := outboxes[i]
		if !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
			continue
		}

		summary.alarmCount++
		if summary.channelID == "" {
			summary.channelID = strings.TrimSpace(outbox.ChannelID)
		}
		if summary.alarmType == "" {
			summary.alarmType = outbox.Kind.ToAlarmType()
		}

		roomID := strings.TrimSpace(rows[i].RoomID)
		if roomID != "" {
			summary.uniqueRooms[roomID] = struct{}{}
		}
	}

	return summary
}

func deliveryResultCounts(sendResult string, alarmCount, roomCount int) (int, int, int, int) {
	switch strings.TrimSpace(sendResult) {
	case "success":
		return alarmCount, 0, roomCount, 0
	case "failure":
		return 0, alarmCount, 0, roomCount
	default:
		return 0, 0, 0, 0
	}
}

func (d *Dispatcher) logCommunityShortsDeliveryAudit(
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
	deliveryPath := normalizeCommunityShortsDeliveryPath(communityShortsDeliveryPath)
	events := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, limit)
	limitedRows := rows[:limit]
	limitedOutboxes := outboxes[:limit]
	for i := range limitedOutboxes {
		outbox := limitedOutboxes[i]
		if !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
			continue
		}

		attemptFinishedAt := sentAt.UTC()
		events = append(events, domain.YouTubeNotificationDeliveryTelemetry{
			DeliveryID:        limitedRows[i].ID,
			AttemptOrdinal:    deliveryAttemptOrdinal(limitedRows[i]),
			OutboxID:          outbox.ID,
			ChannelID:         outbox.ChannelID,
			ContentID:         strings.TrimSpace(outbox.ContentID),
			PostID:            resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload),
			RoomID:            limitedRows[i].RoomID,
			AlarmType:         outbox.Kind.ToAlarmType(),
			DedupeKey:         dedupeKeyLogValue(outbox),
			DeliveryPath:      deliveryPath,
			DeliveryMode:      deliveryMode,
			SendResult:        sendResult,
			FailureReason:     truncateString(strings.TrimSpace(failureReason), 100),
			AttemptStartedAt:  deliveryAttemptStartedAt(limitedRows[i]),
			AttemptFinishedAt: &attemptFinishedAt,
			EventAt:           attemptFinishedAt,
			NextAttemptAt:     time.Now().UTC(),
		})
	}
	if len(events) == 0 {
		return
	}

	preparedEvents := events
	if d.telemetry != nil {
		prepared, err := d.telemetry.prepareRows(ctx, events)
		if err != nil {
			d.logger.Warn("Failed to enrich persistent delivery audit",
				slog.Int("events", len(events)),
				slog.Any("error", err))
		} else {
			preparedEvents = prepared
			enqueueErr := d.telemetry.enqueuePrepared(ctx, preparedEvents)
			if enqueueErr == nil {
				if err := d.telemetry.PersistPostLatencyClassificationsByOutboxIDs(ctx, collectTelemetryOutboxIDs(preparedEvents)); err != nil {
					d.logger.Warn("Failed to persist post latency classifications",
						slog.Int("events", len(preparedEvents)),
						slog.Any("error", err))
				}
				return
			}
			d.logger.Warn("Failed to enqueue persistent delivery audit",
				slog.Int("events", len(preparedEvents)),
				slog.Any("error", enqueueErr))
		}
	}

	fallbackClassificationsByOutboxID, err := d.loadDeliveryTelemetryLatencyClassifications(ctx, preparedEvents)
	if err != nil {
		d.logger.Warn("Failed to load fallback delivery telemetry latency classifications",
			slog.Int("events", len(preparedEvents)),
			slog.Any("error", err))
	}

	for i := range preparedEvents {
		attrs := buildDeliveryAuditLogAttrsWithClassification(preparedEvents[i], fallbackClassificationsByOutboxID[preparedEvents[i].OutboxID])
		attrs = append(attrs, slog.String(logschema.FieldTelemetrySource, "direct_fallback"))
		if sendErr != nil {
			attrs = append(attrs, slog.String("error", sendErr.Error()))
		}

		d.logger.Info(deliveryAuditLogMessage, attrs...)
	}
}
