package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/telemetry"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
)

const (
	sendResultSuccess = "success"
	sendResultFailure = "failure"

	deliveryModeGrouped = "grouped"
	deliveryModePerRoom = "per_room"
)

type AuditLogger struct {
	delivery *store.DeliveryRepository
	logger   *slog.Logger
	config   Config
}

func newAuditLogger(
	deliveryRepo *store.DeliveryRepository,
	logger *slog.Logger,
	config *Config,
) *AuditLogger {
	return &AuditLogger{
		delivery: deliveryRepo,
		logger:   logger,
		config:   *config,
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

	deliveryPath := telemetry.CommunityShortsDeliveryPath
	limitedRows := rows[:limit]
	limitedOutboxes := outboxes[:limit]

	for i := range limitedOutboxes {
		outbox := limitedOutboxes[i]
		if !telemetry.IsCommunityShortsDeliveryAuditKind(outbox.Kind) {
			continue
		}

		al.logger.Info(CommunityShortsDeliveryAttemptMessage,
			slog.Int64(FieldDeliveryID, limitedRows[i].ID),
			slog.Int64(FieldOutboxID, outbox.ID),
			privacylog.RoomIDAttr(limitedRows[i].RoomID),
			slog.String(FieldChannelID, outbox.ChannelID),
			slog.String(FieldPostID, telemetry.PostIDLogValue(outbox.Kind, outbox.ContentID, outbox.Payload)),
			slog.String(FieldContentID, strings.TrimSpace(outbox.ContentID)),
			slog.String(FieldAlarmType, string(outbox.Kind.ToAlarmType())),
			slog.Time(FieldAttemptStartedAt, attemptStartedAt),
			slog.Int(FieldAttemptOrdinal, deliveryAttemptOrdinal(&limitedRows[i])),
			slog.String(FieldDeliveryPath, deliveryPath),
			slog.String(FieldDeliveryMode, deliveryMode),
			slog.String(FieldDedupeKey, telemetry.DedupeKeyLogValue(&outbox)),
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

	deliveryPath := telemetry.CommunityShortsDeliveryPath
	summary := summarizeCommunityShortsDeliveryResult(rows[:limit], outboxes[:limit])

	if summary.alarmCount == 0 {
		return
	}

	roomCount := len(summary.uniqueRooms)
	successfulAlarmCount, failedAlarmCount, successfulRoomCount, failedRoomCount := deliveryResultCounts(sendResult, summary.alarmCount, roomCount)

	attrs := []any{
		slog.String(FieldChannelID, summary.channelID),
		slog.String(FieldAlarmType, string(summary.alarmType)),
		slog.Time(FieldSentAt, sentAt),
		slog.String(FieldSendResult, sendResult),
		slog.String(FieldDeliveryPath, deliveryPath),
		slog.String(FieldDeliveryMode, deliveryMode),
		slog.Int(FieldTargetAlarmCount, summary.alarmCount),
		slog.Int(FieldSuccessfulAlarmCount, successfulAlarmCount),
		slog.Int(FieldFailedAlarmCount, failedAlarmCount),
		slog.Int(FieldTargetRoomCount, roomCount),
		slog.Int(FieldSuccessfulRoomCount, successfulRoomCount),
		slog.Int(FieldFailedRoomCount, failedRoomCount),
	}
	if roomCount == 1 {
		for roomID := range summary.uniqueRooms {
			attrs = append(attrs, privacylog.RoomIDAttr(roomID))
			break
		}
	}

	if trimmedReason := strings.TrimSpace(failureReason); trimmedReason != "" {
		attrs = append(attrs, slog.String(FieldFailureReason, deliverysql.TruncateString(trimmedReason, 100)))
	}

	al.logger.Info(CommunityShortsDeliveryResultMessage, attrs...)
}

func (al *AuditLogger) logFinalizedCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) error {
	if al == nil || al.delivery == nil || al.logger == nil {
		return nil
	}

	results, err := al.delivery.LoadTerminalCommunityShortsOutboxResults(ctx, outboxIDs)
	if err != nil {
		return fmt.Errorf("load terminal community shorts outbox results: %w", err)
	}

	if len(results) == 0 {
		return nil
	}

	trackingErr := al.delivery.AttachTerminalCommunityShortsTracking(ctx, results)
	if trackingErr != nil {
		al.logger.Warn(CommunityShortsFinalResultTrackingUnavailableMessage, slog.Any("outbox_ids", outboxIDs), slog.Any("error", trackingErr))
	}

	finalizedAt := time.Now().UTC()

	for i := range results {
		timing := alarmtiming.Snapshot{}

		if trackingErr == nil {
			timing = finalizedCommunityShortsAlarmTiming(&results[i])
		}

		al.logFinalizedCommunityShortsOutboxResult(&results[i], finalizedAt, timing)
	}

	return nil
}

func (al *AuditLogger) logFinalizedCommunityShortsOutboxResult(
	result *store.TerminalCommunityShortsOutboxResult,
	finalizedAt time.Time,
	timing alarmtiming.Snapshot,
) {
	sendResult := sendResultFailure
	eventAt := finalizedAt

	if result.Status == domain.OutboxStatusSent {
		sendResult = sendResultSuccess

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
		slog.Int64(FieldOutboxID, result.OutboxID),
		slog.String(FieldChannelID, result.ChannelID),
		slog.String(FieldPostID, telemetry.PostIDLogValue(result.Kind, result.ContentID, result.Payload)),
		slog.String(FieldContentID, result.ContentID),
		slog.String(FieldAlarmType, string(result.Kind.ToAlarmType())),
		slog.Time(FieldSentAt, eventAt),
		slog.String(FieldSendResult, sendResult),
		slog.String(FieldDeliveryPath, telemetry.CommunityShortsDeliveryPath),
		slog.String(FieldDeliveryMode, DeliveryModeFinalResult),
		slog.String(FieldDedupeKey, telemetry.DedupeKeyLogValue(&outbox)),
		slog.String(FieldTelemetrySource, TelemetrySourceOutboxFinalResult),
		slog.Int(FieldTargetRoomCount, result.TargetRoomCount),
		slog.Int(FieldSuccessfulRoomCount, result.SuccessfulRoomCount),
		slog.Int(FieldFailedRoomCount, result.FailedRoomCount),
	}

	attrs = appendCommunityShortsAlarmTimingLogAttrs(attrs, timing)

	if result.AggregatedFailReason != "" {
		attrs = append(attrs, slog.String(FieldFailureReason, result.AggregatedFailReason))
	}

	al.logger.Info(CommunityShortsDeliveryAuditMessage, attrs...)
}
