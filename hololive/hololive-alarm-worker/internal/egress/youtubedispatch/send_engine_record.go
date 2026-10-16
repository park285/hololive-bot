package youtubedispatch

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

// 실패 기록 wrapper는 claim 해제(DB 상태 변경)를 먼저 수행하고, 확정된 결과만 MetricsRecorder에 넘긴다.
// recorder는 로그·audit·결과 집계만 맡는다.

func (d *SendEngine) releasePerRoomClaims(ctx context.Context, row *domain.YouTubeNotificationDelivery, claimTokens []dispatchstate.ClaimToken, cause string) {
	d.claims.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release per-room delivery claims after "+cause,
		slog.Int64("delivery_id", row.ID),
		slog.Int64("outbox_id", row.OutboxID),
	)
}

func (d *SendEngine) releaseGroupedClaims(ctx context.Context, group *deliveryGroup, claimTokens []dispatchstate.ClaimToken, cause string) {
	roomID, channelID, _ := groupedDeliveryFields(group)
	d.claims.releaseDeliveryClaimsWithWarning(ctx, claimTokens, "Failed to release grouped delivery claims after "+cause,
		privacylog.RoomIDAttr(roomID),
		slog.String("channel_id", channelID),
	)
}

func (d *SendEngine) recordPerRoomFormatFailure(
	ctx context.Context,
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.releasePerRoomClaims(ctx, &attempt.rows[0], attempt.claimTokens, "format error")
	d.metricsRecorder.recordPerRoomFormatFailure(&attempt.rows[0], attempt.rows, attempt.outboxes, result)
}

func (d *SendEngine) recordGroupedFormatFailure(
	ctx context.Context,
	group *deliveryGroup,
	attempt *deliveryAttempt,
	err error,
	result *deliveryDispatchResult,
) {
	d.releaseGroupedClaims(ctx, group, attempt.claimTokens, "format error")
	d.metricsRecorder.recordGroupedFormatFailure(group, attempt.rows, attempt.outboxes, err, result)
}

func (d *SendEngine) recordPerRoomMissingMessage(
	ctx context.Context,
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.releasePerRoomClaims(ctx, &attempt.rows[0], attempt.claimTokens, "missing preformatted message")
	d.metricsRecorder.recordPerRoomMissingMessage(&attempt.rows[0], result)
}

func (d *SendEngine) recordPerRoomRequestBuildFailure(
	ctx context.Context,
	attempt *deliveryAttempt,
	err error,
	result *deliveryDispatchResult,
) {
	d.releasePerRoomClaims(ctx, &attempt.rows[0], attempt.claimTokens, "request build error")
	d.metricsRecorder.recordPerRoomRequestBuildFailure(&attempt.rows[0], &attempt.outboxes[0], attempt.rows, attempt.outboxes, err, result)
}

func (d *SendEngine) recordPerRoomSendFailure(
	ctx context.Context,
	attempt *deliveryAttempt,
	sendErr error,
	result *deliveryDispatchResult,
) {
	d.releasePerRoomClaims(ctx, &attempt.rows[0], attempt.claimTokens, "send failure")
	d.metricsRecorder.recordPerRoomSendFailure(&attempt.rows[0], attempt.rows, attempt.outboxes, attempt.request, sendErr, result)
}

func (d *SendEngine) recordPerRoomSuccess(
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordPerRoomSuccess(&attempt.rows[0], attempt.rows, attempt.outboxes, attempt.request, attempt.claimTokens, result)
}

func (d *SendEngine) recordDeliveryFailure(
	result *deliveryDispatchResult,
	reason string,
	deliveryID, outboxID int64,
) {
	d.metricsRecorder.recordDeliveryFailure(result, reason, deliveryID, outboxID)
}

func (d *SendEngine) recordGroupedRequestBuildFailure(
	ctx context.Context,
	group *deliveryGroup,
	attempt *deliveryAttempt,
	err error,
	result *deliveryDispatchResult,
) {
	d.releaseGroupedClaims(ctx, group, attempt.claimTokens, "request build error")
	d.metricsRecorder.recordGroupedRequestBuildFailure(group, attempt.rows, attempt.outboxes, err, result)
}

func (d *SendEngine) recordGroupedSendFailure(
	ctx context.Context,
	group *deliveryGroup,
	attempt *deliveryAttempt,
	sendErr error,
	result *deliveryDispatchResult,
) {
	d.releaseGroupedClaims(ctx, group, attempt.claimTokens, "send failure")
	d.metricsRecorder.recordGroupedSendFailure(group, attempt.rows, attempt.outboxes, attempt.request, sendErr, result)
}

func (d *SendEngine) recordGroupedSuccess(
	group *deliveryGroup,
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordGroupedSuccess(group, attempt.rows, attempt.outboxes, attempt.request, attempt.claimTokens, result)
}
