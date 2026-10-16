package youtubedispatch

import (
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
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
	mr.recordDeliveryFailure(result, "format message", row.ID, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomMissingMessage(
	row *domain.YouTubeNotificationDelivery,
	result *deliveryDispatchResult,
) {
	mr.recordDeliveryFailure(result, "outbox row not found", row.ID, row.OutboxID)
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
	mr.recordDeliveryFailure(result, "dedupe key", row.ID, row.OutboxID)
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
	mr.recordDeliveryFailureWithRetryAfter(result, reason, row.ID, row.OutboxID, deliveryRetryAfter(sendErr))
}

func (mr *MetricsRecorder) recordPerRoomSuccess(
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	result *deliveryDispatchResult,
) {
	sentAt := time.Now()

	mr.logger.Debug("Sent per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		privacylog.RoomIDAttr(row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, "per_room", "success", "")

	result.mu.Lock()

	result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, row.ID)
	result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, row.OutboxID)
	result.SuccessClaimTokens = append(result.SuccessClaimTokens, claimTokens...)
	result.mu.Unlock()
}
