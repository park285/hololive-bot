package producerruntime

const (
	EventIngestionRuntimeConfigured = "ingestion.runtime.configured"
	EventIngestionRuntimeStarted    = "ingestion.runtime.started"
	EventIngestionRuntimeStopped    = "ingestion.runtime.stopped"

	EventYouTubePollStarted   = "youtube.poll.iteration.started"
	EventYouTubePollSucceeded = "youtube.poll.iteration.succeeded"
	EventYouTubePollFailed    = "youtube.poll.iteration.failed"

	EventYouTubeOutboxWriteStarted   = "youtube.outbox.write.started"
	EventYouTubeOutboxWriteSucceeded = "youtube.outbox.write.succeeded"
	EventYouTubeOutboxWriteFailed    = "youtube.outbox.write.failed"

	EventPhotoSyncStarted   = "photo.sync.iteration.started"
	EventPhotoSyncSucceeded = "photo.sync.iteration.succeeded"
	EventPhotoSyncFailed    = "photo.sync.iteration.failed"
)
