package youtubedispatch

import (
	"context"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
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
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
	)
}

func (d *SendEngine) recordPerRoomFormatFailure(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.releasePerRoomClaims(ctx, row, claimTokens, "format error")
	d.metricsRecorder.recordPerRoomFormatFailure(row, rows, outboxes, result, mu)
}

func (d *SendEngine) recordGroupedFormatFailure(
	ctx context.Context,
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.releaseGroupedClaims(ctx, group, claimTokens, "format error")
	d.metricsRecorder.recordGroupedFormatFailure(group, validRows, validOutboxes, err, result, mu)
}

func (d *SendEngine) recordPerRoomMissingMessage(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.releasePerRoomClaims(ctx, row, claimTokens, "missing preformatted message")
	d.metricsRecorder.recordPerRoomMissingMessage(row, result, mu)
}

func (d *SendEngine) recordPerRoomRequestBuildFailure(
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
	d.releasePerRoomClaims(ctx, row, claimTokens, "request build error")
	d.metricsRecorder.recordPerRoomRequestBuildFailure(row, outbox, rows, outboxes, err, result, mu)
}

func (d *SendEngine) recordPerRoomSendFailure(
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
	d.releasePerRoomClaims(ctx, row, claimTokens, "send failure")
	d.metricsRecorder.recordPerRoomSendFailure(row, rows, outboxes, sendReq, sendErr, result, mu)
}

func (d *SendEngine) recordPerRoomSuccess(
	row *domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordPerRoomSuccess(row, rows, outboxes, sendReq, claimTokens, result, mu)
}

func (d *SendEngine) recordDeliveryFailure(
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	d.metricsRecorder.recordDeliveryFailure(result, mu, reason, deliveryID, outboxID)
}

func (d *SendEngine) recordGroupedRequestBuildFailure(
	ctx context.Context,
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	claimTokens []dispatchstate.ClaimToken,
	err error,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.releaseGroupedClaims(ctx, group, claimTokens, "request build error")
	d.metricsRecorder.recordGroupedRequestBuildFailure(group, validRows, validOutboxes, err, result, mu)
}

func (d *SendEngine) recordGroupedSendFailure(
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
	d.releaseGroupedClaims(ctx, group, claimTokens, "send failure")
	d.metricsRecorder.recordGroupedSendFailure(group, validRows, validOutboxes, sendReq, sendErr, result, mu)
}

func (d *SendEngine) recordGroupedSuccess(
	group *deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []dispatchstate.ClaimToken,
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordGroupedSuccess(group, validRows, validOutboxes, sendReq, claimTokens, result, mu)
}
