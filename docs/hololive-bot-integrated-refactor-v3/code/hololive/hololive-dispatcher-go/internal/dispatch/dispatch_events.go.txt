package dispatch

const (
	EventDispatchBatchDrainStarted   = "dispatch.batch.drain.started"
	EventDispatchBatchDrainSucceeded = "dispatch.batch.drain.succeeded"
	EventDispatchBatchDrainFailed    = "dispatch.batch.drain.failed"

	EventDispatchGroupRenderStarted   = "dispatch.group.render.started"
	EventDispatchGroupRenderSucceeded = "dispatch.group.render.succeeded"
	EventDispatchGroupRenderFailed    = "dispatch.group.render.failed"

	EventDispatchGroupSendStarted   = "dispatch.group.send.started"
	EventDispatchGroupSendSucceeded = "dispatch.group.send.succeeded"
	EventDispatchGroupSendFailed    = "dispatch.group.send.failed"

	EventDispatchGroupMarkSendingFailed    = "dispatch.group.mark_sending.failed"
	EventDispatchGroupMarkDispatchedFailed = "dispatch.group.mark_dispatched.failed"

	EventDispatchRetryScheduled              = "dispatch.group.retry.scheduled"
	EventDispatchRetryScheduleFailed         = "dispatch.group.retry.schedule_failed"
	EventDispatchDLQMoved                    = "dispatch.group.dlq.moved"
	EventDispatchDLQMoveFailed               = "dispatch.group.dlq.move_failed"
	EventDispatchQuarantined                 = "dispatch.group.quarantined"
	EventDispatchPersistenceFallbackRequeued = "dispatch.group.persistence_fallback.requeued"
	EventDispatchPersistenceFallbackFailed   = "dispatch.group.persistence_fallback.failed"
)
