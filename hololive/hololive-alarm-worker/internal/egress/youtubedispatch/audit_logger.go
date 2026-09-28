package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/logschema"
	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
	deliverytimeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
)

const (
	sendResultSuccess = "success"
	sendResultFailure = "failure"

	deliveryModeGrouped = string(store.DeliveryModeGrouped)
	deliveryModePerRoom = string(store.DeliveryModePerRoom)
)

// AuditLogger는 시도 시작·결과 요약·최종 결과 로그만 남긴다. 시도 telemetry 행은 TransitionStore가 lifecycle 전이
// 트랜잭션 안에서 한 번 기록하며, 여기서 하던 prepare·enqueue·direct_fallback 로그와 commit 뒤 분류 재저장은
// DEC-20260926-hololive-delivery-telemetry-single-path로 삭제했다.
type AuditLogger struct {
	telemetry *telemetry.Repository
	delivery  *store.DeliveryRepository
	logger    *slog.Logger
	config    dispatchstate.Config
}

func newAuditLogger(
	telemetryRepo *telemetry.Repository,
	deliveryRepo *store.DeliveryRepository,
	logger *slog.Logger,
	config *dispatchstate.Config,
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

		al.logger.Info(deliveryAttemptStartedLogMessage,
			slog.Int64(logschema.FieldDeliveryID, limitedRows[i].ID),
			slog.Int64(logschema.FieldOutboxID, outbox.ID),
			slog.String(logschema.FieldRoomID, limitedRows[i].RoomID),
			slog.String(logschema.FieldChannelID, outbox.ChannelID),
			slog.String(deliveryAuditPostIDLogField, telemetry.PostIDLogValue(outbox.Kind, outbox.ContentID, outbox.Payload)),
			slog.String(deliveryAuditContentIDLogField, strings.TrimSpace(outbox.ContentID)),
			slog.String(deliveryAuditAlarmTypeLogField, string(outbox.Kind.ToAlarmType())),
			slog.Time(deliveryAttemptStartedAtLogField, attemptStartedAt),
			slog.Int(logschema.FieldAttemptOrdinal, deliveryAttemptOrdinal(&limitedRows[i])),
			slog.String(deliveryAuditPathLogField, deliveryPath),
			slog.String(deliveryAuditModeLogField, deliveryMode),
			slog.String(deliveryDedupeKeyLogField, telemetry.DedupeKeyLogValue(&outbox)),
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
		slog.Int64(logschema.FieldOutboxID, result.OutboxID),
		slog.String(logschema.FieldChannelID, result.ChannelID),
		slog.String(deliveryAuditPostIDLogField, telemetry.PostIDLogValue(result.Kind, result.ContentID, result.Payload)),
		slog.String(deliveryAuditContentIDLogField, result.ContentID),
		slog.String(deliveryAuditAlarmTypeLogField, string(result.Kind.ToAlarmType())),
		slog.Time(deliveryAuditSentAtLogField, eventAt),
		slog.String(deliveryAuditSendResultLogField, sendResult),
		slog.String(deliveryAuditPathLogField, telemetry.CommunityShortsDeliveryPath),
		slog.String(deliveryAuditModeLogField, logschema.DeliveryModeFinalResult),
		slog.String(deliveryDedupeKeyLogField, telemetry.DedupeKeyLogValue(&outbox)),
		slog.String(logschema.FieldTelemetrySource, logschema.TelemetrySourceOutboxFinalResult),
		slog.Int(logschema.FieldTargetRoomCount, result.TargetRoomCount),
		slog.Int(logschema.FieldSuccessfulRoomCount, result.SuccessfulRoomCount),
		slog.Int(logschema.FieldFailedRoomCount, result.FailedRoomCount),
	}

	attrs = appendCommunityShortsAlarmTimingLogAttrs(attrs, timing)

	if result.AggregatedFailReason != "" {
		attrs = append(attrs, slog.String(deliveryAuditFailureReasonLogField, result.AggregatedFailReason))
	}

	attrs = appendLatencyClassificationLogAttr(attrs, &result.LatencyClassification)

	al.logger.Info(deliveryAuditLogMessage, attrs...)
}
