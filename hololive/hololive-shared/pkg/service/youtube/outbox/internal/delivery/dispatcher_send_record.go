package delivery

import (
	"context"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *SendEngine) recordPerRoomFormatFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordPerRoomFormatFailure(ctx, row, rows, outboxes, claimTokens, result, mu)
}

func (d *SendEngine) recordPerRoomMissingMessage(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordPerRoomMissingMessage(ctx, row, claimTokens, result, mu)
}

func (d *SendEngine) recordPerRoomRequestBuildFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordPerRoomRequestBuildFailure(ctx, row, outbox, rows, outboxes, claimTokens, err, result, mu)
}

func (d *SendEngine) recordPerRoomSendFailure(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	sendErr error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordPerRoomSendFailure(ctx, row, rows, outboxes, sendReq, claimTokens, sendErr, result, mu)
}

func (d *SendEngine) recordPerRoomSuccess(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordPerRoomSuccess(ctx, row, rows, outboxes, sendReq, claimTokens, result, mu)
}

func (d *SendEngine) recordDeliveryFailure(
	result *deliveryDispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	d.metricsRecorder.recordDeliveryFailure(result, mu, reason, deliveryID, outboxID)
}

func (d *SendEngine) recordGroupedRequestBuildFailure(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	claimTokens []deliveryClaimToken,
	err error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordGroupedRequestBuildFailure(ctx, group, validRows, validOutboxes, claimTokens, err, result, mu)
}

func (d *SendEngine) recordGroupedSendFailure(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	sendErr error,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordGroupedSendFailure(ctx, group, validRows, validOutboxes, sendReq, claimTokens, sendErr, result, mu)
}

func (d *SendEngine) recordGroupedSuccess(
	ctx context.Context,
	group deliveryGroup,
	validRows []domain.YouTubeNotificationDelivery,
	validOutboxes []domain.YouTubeNotificationOutbox,
	sendReq deliverySendRequest,
	claimTokens []deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.metricsRecorder.recordGroupedSuccess(ctx, group, validRows, validOutboxes, sendReq, claimTokens, result, mu)
}
