package youtubedispatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func (mr *MetricsRecorder) recordKaringRequestBuildFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release Karing delivery claims after request build error",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	failedAt := time.Now()
	mr.logger.Warn("Failed to build Karing delivery request",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(outboxes)),
		dedupeKeyLogAttrForOutboxes(outboxes),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, mode, "failure", "karing request", err)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, mode, "failure", "karing request")
	for i := range rows {
		mr.recordDeliveryFailure(result, mu, "karing request", rows[i].ID, rows[i].OutboxID)
	}
}

func (mr *MetricsRecorder) recordKaringSendFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release Karing delivery claims after send failure",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
	failedAt := time.Now()
	mr.logger.Warn("Failed to send Karing delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, mode, "failure", "karing send", err)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, mode, "failure", "karing send")
	for i := range rows {
		mr.recordDeliveryFailureWithRetryAfter(result, mu, "karing send", rows[i].ID, rows[i].OutboxID, deliveryRetryAfter(err))
	}
}

func (mr *MetricsRecorder) recordKaringSuccess(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	mode string,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	sentAt := time.Now()
	mr.logger.Info("Sent Karing delivery",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.String("delivery_mode", mode),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttr(sendReq.dedupeKeys))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, sentAt, mode, "success", "", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, sentAt, mode, "success", "")

	mu.Lock()
	for i := range rows {
		result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, rows[i].ID)
		result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, rows[i].OutboxID)
	}
	result.SuccessClaimTokens = append(result.SuccessClaimTokens, claimTokens...)
	mu.Unlock()
}
