package youtubedispatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func (mr *MetricsRecorder) recordGroupedRequestBuildFailure(
	ctx context.Context,
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after request build error",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	failedAt := time.Now()
	mr.logger.Warn("Failed to build grouped delivery request",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validOutboxes)),
		dedupeKeyLogAttrForOutboxes(validOutboxes),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key", err)
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", "dedupe key")
	for i := range validRows {
		mr.recordDeliveryFailure(result, mu, "dedupe key", validRows[i].ID, validRows[i].OutboxID)
	}
}

func (mr *MetricsRecorder) recordGroupedSendFailure(
	ctx context.Context,
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	sendErr error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after send failure",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	failedAt := time.Now()
	reason := deliveryFailureReason(sendErr)
	mr.logger.Warn("Failed to send grouped delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", sendErr))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, failedAt, "grouped", "failure", reason, sendErr)
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, failedAt, "grouped", "failure", reason)
	for i := range validRows {
		mr.recordDeliveryFailureWithRetryAfter(result, mu, reason, validRows[i].ID, validRows[i].OutboxID, deliveryRetryAfter(sendErr))
	}
}

func (mr *MetricsRecorder) recordGroupedSuccess(
	ctx context.Context,
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	roomID, channelID, kind := groupedDeliveryFields(group)
	sentAt := time.Now()
	mr.logger.Info("Sent grouped delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(validRows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, validRows, validOutboxes, sentAt, "grouped", "success", "", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(validRows, validOutboxes, sentAt, "grouped", "success", "")

	mu.Lock()
	for i := range validRows {
		result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, validRows[i].ID)
		result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, validRows[i].OutboxID)
	}
	result.SuccessClaimTokens = append(result.SuccessClaimTokens, claimTokens...)
	mu.Unlock()
}

func groupedDeliveryFields(group *deliveryGroup) (result1, result2 string, result3 domain.OutboxKind) {
	if group == nil {
		return "", "", ""
	}
	return group.roomID, group.channelID, group.kind
}
