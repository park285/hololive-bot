package dispatch

import (
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/telemetry"
)

func dedupeKeyLogAttrForOutboxes(outboxes []domain.YouTubeNotificationOutbox) slog.Attr {
	dedupeKeys := make([]string, 0, len(outboxes))
	for i := range outboxes {
		dedupeKeys = append(dedupeKeys, telemetry.DedupeKeyLogValue(&outboxes[i]))
	}
	return dedupeKeyLogAttr(dedupeKeys)
}

func deliveryAttemptOrdinal(row *domain.YouTubeNotificationDelivery) int {
	attemptOrdinal := row.AttemptCount + 1
	if attemptOrdinal <= 0 {
		return 1
	}
	return attemptOrdinal
}

func deliveryAttemptStartedAt(row *domain.YouTubeNotificationDelivery) *time.Time {
	if row.LockedAt == nil || row.LockedAt.IsZero() {
		return nil
	}

	startedAt := row.LockedAt.UTC()
	return &startedAt
}

func (d *SendEngine) logCommunityShortsDeliveryAttemptStarted(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	attemptStartedAt time.Time,
	deliveryMode string,
) {
	d.auditLogger.logCommunityShortsDeliveryAttemptStarted(rows, outboxes, attemptStartedAt, deliveryMode)
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
		collectCommunityShortsDeliveryResultSummary(&summary, &rows[i], &outboxes[i])
	}

	return summary
}

func collectCommunityShortsDeliveryResultSummary(
	summary *communityShortsDeliveryResultSummary,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
) {
	if !telemetry.IsCommunityShortsDeliveryAuditKind(outbox.Kind) {
		return
	}

	summary.alarmCount++
	if summary.channelID == "" {
		summary.channelID = strings.TrimSpace(outbox.ChannelID)
	}
	if summary.alarmType == "" {
		summary.alarmType = outbox.Kind.ToAlarmType()
	}

	roomID := strings.TrimSpace(row.RoomID)
	if roomID != "" {
		summary.uniqueRooms[roomID] = struct{}{}
	}
}

func deliveryResultCounts(sendResult string, alarmCount, roomCount int) (result1, result2, result3, result4 int) {
	switch strings.TrimSpace(sendResult) {
	case "success":
		return alarmCount, 0, roomCount, 0
	case "failure":
		return 0, alarmCount, 0, roomCount
	default:
		return 0, 0, 0, 0
	}
}

func buildCommunityShortsDeliveryAuditEvents(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryPath string,
	deliveryMode string,
	sendResult string,
	failureReason string,
) []domain.YouTubeNotificationDeliveryTelemetry {
	events := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, len(outboxes))
	for i := range outboxes {
		if !telemetry.IsCommunityShortsDeliveryAuditKind(outboxes[i].Kind) {
			continue
		}
		events = append(events, buildCommunityShortsDeliveryAuditEvent(&rows[i], &outboxes[i], sentAt,
			deliveryPath,
			deliveryMode,
			sendResult,
			failureReason,
		))
	}
	return events
}

func buildCommunityShortsDeliveryAuditEvent(
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryPath string,
	deliveryMode string,
	sendResult string,
	failureReason string,
) domain.YouTubeNotificationDeliveryTelemetry {
	attemptFinishedAt := sentAt.UTC()
	return domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:        row.ID,
		AttemptOrdinal:    deliveryAttemptOrdinal(row),
		OutboxID:          outbox.ID,
		ChannelID:         outbox.ChannelID,
		ContentID:         strings.TrimSpace(outbox.ContentID),
		PostID:            telemetry.ResolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload),
		RoomID:            row.RoomID,
		AlarmType:         outbox.Kind.ToAlarmType(),
		DedupeKey:         telemetry.DedupeKeyLogValue(outbox),
		DeliveryPath:      deliveryPath,
		DeliveryMode:      deliveryMode,
		SendResult:        sendResult,
		FailureReason:     deliverysql.TruncateString(strings.TrimSpace(failureReason), 100),
		AttemptStartedAt:  deliveryAttemptStartedAt(row),
		AttemptFinishedAt: &attemptFinishedAt,
		EventAt:           attemptFinishedAt,
		NextAttemptAt:     time.Now().UTC(),
	}
}
