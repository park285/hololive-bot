package youtubedispatch

import (
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
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

func deliveryResultCounts(sendResult string, alarmCount, roomCount int) (successAlarms, failedAlarms, successRooms, failedRooms int) {
	switch strings.TrimSpace(sendResult) {
	case sendResultSuccess:
		return alarmCount, 0, roomCount, 0
	case sendResultFailure:
		return 0, alarmCount, 0, roomCount
	default:
		return 0, 0, 0, 0
	}
}
