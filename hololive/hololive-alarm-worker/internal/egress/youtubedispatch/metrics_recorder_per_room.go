package youtubedispatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func (mr *MetricsRecorder) recordPerRoomFormatFailure(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after format error",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", "format message", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "format message")
	mr.recordDeliveryFailure(result, mu, "format message", row.ID, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomMissingMessage(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after missing preformatted message",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	mr.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomRequestBuildFailure(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after request build error",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	mr.logger.Warn("Failed to build per-room delivery request",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttrForOutboxes([]domain.YouTubeNotificationOutbox{*outbox}),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", "dedupe key", err)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", "dedupe key")
	mr.recordDeliveryFailure(result, mu, "dedupe key", row.ID, row.OutboxID)
}

func (mr *MetricsRecorder) recordPerRoomSendFailure(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	sendErr error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after send failure",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
	failedAt := time.Now()
	reason := deliveryFailureReason(sendErr)
	mr.logger.Warn("Failed to send per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "per_room", "failure", reason, sendErr)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "per_room", "failure", reason)
	mr.recordDeliveryFailureWithRetryAfter(result, mu, reason, row.ID, row.OutboxID, deliveryRetryAfter(sendErr))
}

func (mr *MetricsRecorder) recordPerRoomSuccess(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	sentAt := time.Now()
	mr.logger.Info("Sent per-room delivery",
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
		slog.String("room_id", row.RoomID),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, sentAt, "per_room", "success", "", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, "per_room", "success", "")

	mu.Lock()
	result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, row.ID)
	result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, row.OutboxID)
	result.SuccessClaimTokens = append(result.SuccessClaimTokens, claimTokens...)
	mu.Unlock()
}
