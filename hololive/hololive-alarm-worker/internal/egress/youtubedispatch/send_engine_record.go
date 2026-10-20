package youtubedispatch

// 실패·성공 기록 wrapper는 확정된 결과만 MetricsRecorder에 넘긴다.
// recorder는 로그·audit·결과 집계만 맡는다.

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
	attempt *deliveryAttempt,
	result *deliveryDispatchResult,
) {
	d.metricsRecorder.recordPerRoomSuccess(&attempt.rows[0], attempt.rows, attempt.outboxes, attempt.request, result)
}

func (d *SendEngine) recordDeliveryFailure(
	result *deliveryDispatchResult,
	reason string,
	deliveryID, outboxID int64,
) {
	d.metricsRecorder.recordDeliveryFailure(result, reason, deliveryID, outboxID)
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
