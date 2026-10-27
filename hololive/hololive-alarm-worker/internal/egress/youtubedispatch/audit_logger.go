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
	deliverytimeline "github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/timeline"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
)

const (
	sendResultSuccess = "success"
	sendResultFailure = "failure"

	deliveryModeGrouped = string(store.DeliveryModeGrouped)
	deliveryModePerRoom = string(store.DeliveryModePerRoom)
)

// AuditLogger는 시도 시작·결과 요약·최종 결과를 기록한다.
// 시도 telemetry 행은 TransitionStore가 lifecycle 전이 트랜잭션에서 기록한다.
type AuditLogger struct {
	telemetry *telemetry.Repository
	delivery  *store.DeliveryRepository
	logger    *slog.Logger
	config    Config
}

func newAuditLogger(
	telemetryRepo *telemetry.Repository,
	deliveryRepo *store.DeliveryRepository,
	logger *slog.Logger,
	config *Config,
) *AuditLogger {
	return &AuditLogger{
		telemetry: telemetryRepo,
		delivery:  deliveryRepo,
		logger:    logger,
		config:    *config,
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

	timelinesByOutboxID, err := al.loadFinalizedCommunityShortsTimelines(ctx, outboxIDs, len(results))
	if err != nil {
		return fmt.Errorf("load finalized community shorts timelines: %w", err)
	}

	finalizedAt := time.Now().UTC()

	for i := range results {
		al.logFinalizedCommunityShortsOutboxResultWithTimeline(&results[i], timelinesByOutboxID, finalizedAt)
	}

	return nil
}

func (al *AuditLogger) loadFinalizedCommunityShortsTimelines(
	ctx context.Context,
	outboxIDs []int64,
	resultCount int,
) (map[int64]deliverytimeline.PostDeliveryTimeline, error) {
	timelinesByOutboxID := make(map[int64]deliverytimeline.PostDeliveryTimeline, resultCount)

	if al.telemetry == nil {
		return timelinesByOutboxID, nil
	}

	timelines, err := al.telemetry.ListPostDeliveryTimelinesByOutboxIDs(ctx, outboxIDs)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by outbox ids: %w", err)
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
	result *store.TerminalCommunityShortsOutboxResult,
	timelinesByOutboxID map[int64]deliverytimeline.PostDeliveryTimeline,
	finalizedAt time.Time,
) {
	timing := alarmtiming.Build(nil, result.SentAt)
	if timeline, ok := timelinesByOutboxID[result.OutboxID]; ok {
		result.LatencyClassification = timeline.LatencyClassification
		timing = communityShortsAlarmTimingForTimeline(&timeline)
	}

	al.logFinalizedCommunityShortsOutboxResult(result, finalizedAt, timing)
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

	attrs = appendLatencyClassificationLogAttr(attrs, &result.LatencyClassification)

	al.logger.Info(CommunityShortsDeliveryAuditMessage, attrs...)
}
