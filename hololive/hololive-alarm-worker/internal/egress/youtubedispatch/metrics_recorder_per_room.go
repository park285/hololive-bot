package youtubedispatch

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (mr *MetricsRecorder) recordPerRoomFormatFailure(
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	result *deliveryDispatchResult,
) {
	failedAt := time.Now()
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "format message")
	mr.recordDeliveryFailure(result, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomMissingMessage(
	row *domain.YouTubeNotificationDelivery,
	result *deliveryDispatchResult,
) {
	mr.recordDeliveryFailure(result, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomRequestBuildFailure(
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	err error,
	result *deliveryDispatchResult,
) {
	failedAt := time.Now()

	mr.logger.Warn("Failed to build per-room delivery request",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		privacylog.RoomIDAttr(row.RoomID),
		dedupeKeyLogAttrForOutboxes([]domain.YouTubeNotificationOutbox{*outbox}),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "dedupe key")
	mr.recordDeliveryFailure(result, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomSendFailure(
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	sendErr error,
	result *deliveryDispatchResult,
) {
	failedAt := time.Now()
	reason := deliveryFailureReason(sendErr)
	mr.logger.Warn("Failed to send per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		privacylog.RoomIDAttr(row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", reason)
	mr.recordDeliveryFailure(result, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomSuccess(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	result *deliveryDispatchResult,
) {
	sentAt := time.Now()

	// Debug가 꺼진 운영 레벨에서도 발송 성공마다 room 가명(HMAC)과 dedupe key 복제를 계산하던 비용을 없앤다.
	// 운영 핸들러의 Enabled는 ctx와 무관하므로 기록 여부는 그대로이며, Handle에 넘기는 ctx(Background)를
	// 바꾸지 않으려고 DebugContext가 아닌 Debug를 유지한다.
	if mr.logger.Enabled(ctx, slog.LevelDebug) {
		mr.logger.Debug("Sent per-room delivery",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
			privacylog.RoomIDAttr(row.RoomID),
			dedupeKeyLogAttr(sendReq.dedupeKeys))
	}

	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, "per_room", "success", "")

	result.mu.Lock()

	result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, row.ID)
	result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, row.OutboxID)
	result.mu.Unlock()
}
