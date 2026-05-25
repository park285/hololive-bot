package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
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

func (d *SendEngine) logCommunityShortsDeliveryAttemptStarted(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	attemptStartedAt time.Time,
	deliveryMode string,
) {
	d.auditLogger.logCommunityShortsDeliveryAttemptStarted(rows, outboxes, attemptStartedAt, deliveryMode)
}

func (d *SendEngine) logCommunityShortsDeliveryResult(
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryMode string,
	sendResult string,
	failureReason string,
) {
	d.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, deliveryMode, sendResult, failureReason)
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
		collectCommunityShortsDeliveryResultSummary(&summary, rows[i], outboxes[i])
	}

	return summary
}

func collectCommunityShortsDeliveryResultSummary(
	summary *communityShortsDeliveryResultSummary,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
) {
	if !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
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

func (d *SendEngine) logCommunityShortsDeliveryAudit(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sentAt time.Time,
	deliveryMode string,
	sendResult string,
	failureReason string,
	sendErr error,
) {
	d.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, sentAt, deliveryMode, sendResult, failureReason, sendErr)
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
		if !isCommunityShortsDeliveryAuditKind(outboxes[i].Kind) {
			continue
		}
		events = append(events, buildCommunityShortsDeliveryAuditEvent(
			rows[i],
			outboxes[i],
			sentAt,
			deliveryPath,
			deliveryMode,
			sendResult,
			failureReason,
		))
	}
	return events
}

func buildCommunityShortsDeliveryAuditEvent(
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
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
		PostID:            resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload),
		RoomID:            row.RoomID,
		AlarmType:         outbox.Kind.ToAlarmType(),
		DedupeKey:         dedupeKeyLogValue(outbox),
		DeliveryPath:      deliveryPath,
		DeliveryMode:      deliveryMode,
		SendResult:        sendResult,
		FailureReason:     truncateString(strings.TrimSpace(failureReason), 100),
		AttemptStartedAt:  deliveryAttemptStartedAt(row),
		AttemptFinishedAt: &attemptFinishedAt,
		EventAt:           attemptFinishedAt,
		NextAttemptAt:     time.Now().UTC(),
	}
}

func (d *SendEngine) prepareCommunityShortsDeliveryAuditEvents(
	ctx context.Context,
	events []domain.YouTubeNotificationDeliveryTelemetry,
) ([]domain.YouTubeNotificationDeliveryTelemetry, bool) {
	return d.auditLogger.prepareCommunityShortsDeliveryAuditEvents(ctx, events)
}

func (d *SendEngine) enqueueCommunityShortsDeliveryAuditEvents(
	ctx context.Context,
	preparedEvents []domain.YouTubeNotificationDeliveryTelemetry,
) bool {
	return d.auditLogger.enqueueCommunityShortsDeliveryAuditEvents(ctx, preparedEvents)
}

func (d *SendEngine) logCommunityShortsDeliveryAuditFallback(
	ctx context.Context,
	preparedEvents []domain.YouTubeNotificationDeliveryTelemetry,
	sendErr error,
) {
	d.auditLogger.logCommunityShortsDeliveryAuditFallback(ctx, preparedEvents, sendErr)
}
