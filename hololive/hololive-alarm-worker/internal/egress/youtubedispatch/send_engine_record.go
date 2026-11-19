package youtubedispatch

import "context"

func (d *SendEngine) recordPerRoomFormatFailure(
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordPerRoomFormatFailure(&attempt.rows[0], attempt.rows, attempt.outboxes, result)
}

func (d *SendEngine) recordGroupedFormatFailure(
	group *deliveryGroup,
	attempt *deliveryAttempt,
	err error,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordGroupedFormatFailure(group, attempt.rows, attempt.outboxes, err, result)
}

func (d *SendEngine) recordPerRoomMissingMessage(
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordPerRoomMissingMessage(&attempt.rows[0], result)
}

func (d *SendEngine) recordPerRoomRequestBuildFailure(
	attempt *deliveryAttempt,
	err error,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordPerRoomRequestBuildFailure(&attempt.rows[0], &attempt.outboxes[0], attempt.rows, attempt.outboxes, err, result)
}

func (d *SendEngine) recordPerRoomSendFailure(
	attempt *deliveryAttempt,
	sendErr error,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordPerRoomSendFailure(&attempt.rows[0], attempt.rows, attempt.outboxes, attempt.request, sendErr, result)
}

func (d *SendEngine) recordPerRoomSuccess(
	ctx context.Context,
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordPerRoomSuccess(ctx, &attempt.rows[0], attempt.rows, attempt.outboxes, attempt.request, result)
}

func (d *SendEngine) recordDeliveryFailure(result *deliveryDispatchResult, outboxID int64) {
	d.metricsRecorder.recordDeliveryFailure(result, outboxID)
}

func (d *SendEngine) recordGroupedRequestBuildFailure(
	group *deliveryGroup,
	attempt *deliveryAttempt,
	err error,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordGroupedRequestBuildFailure(group, attempt.rows, attempt.outboxes, err, result)
}

func (d *SendEngine) recordGroupedSendFailure(
	group *deliveryGroup,
	attempt *deliveryAttempt,
	sendErr error,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordGroupedSendFailure(group, attempt.rows, attempt.outboxes, attempt.request, sendErr, result)
}

func (d *SendEngine) recordGroupedSuccess(
	group *deliveryGroup,
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordGroupedSuccess(group, attempt.rows, attempt.outboxes, attempt.request, result)
}
