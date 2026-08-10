package dispatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func (mr *MetricsRecorder) recordHandoffFailure(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	stage string,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	mr.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release delivery claims after v3 handoff failure",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID))
	failedAt := time.Now()
	mr.logger.Warn("YouTube outbox v3 handoff failed",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.String("stage", stage),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttrForOutboxes(outboxes),
		slog.Any("error", err))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, failedAt, "v3_handoff", "failure", stage, err)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, failedAt, "v3_handoff", "failure", stage)
	for i := range rows {
		mr.recordDeliveryFailure(result, mu, "v3 handoff "+stage, rows[i].ID, rows[i].OutboxID)
	}
}

func (mr *MetricsRecorder) recordHandoffSuccess(
	ctx context.Context,
	roomID string,
	channelID string,
	kind domain.OutboxKind,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	handedOffAt := time.Now()
	mr.logger.Info("YouTube outbox handed off to v3",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.String("kind", string(kind)),
		slog.Int("count", len(rows)),
		dedupeKeyLogAttrForOutboxes(outboxes))
	mr.auditLogger.logCommunityShortsDeliveryAudit(ctx, rows, outboxes, handedOffAt, "v3_handoff", "success", "", nil)
	mr.auditLogger.logCommunityShortsDeliveryResult(rows, outboxes, handedOffAt, "v3_handoff", "success", "")
	mu.Lock()
	for i := range rows {
		result.SuccessDeliveryIDs = append(result.SuccessDeliveryIDs, rows[i].ID)
		result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, rows[i].OutboxID)
	}
	result.SuccessClaimTokens = append(result.SuccessClaimTokens, claimTokens...)
	mu.Unlock()
}
