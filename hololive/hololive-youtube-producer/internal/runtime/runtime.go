package runtime

import producerruntime "github.com/kapu/hololive-youtube-producer/internal/runtime/internal/producerruntime"

type YouTubeProducerRuntime = producerruntime.YouTubeProducerRuntime

const (
	EventIngestionRuntimeConfigured  = producerruntime.EventIngestionRuntimeConfigured
	EventIngestionRuntimeStarted     = producerruntime.EventIngestionRuntimeStarted
	EventIngestionRuntimeStopped     = producerruntime.EventIngestionRuntimeStopped
	EventYouTubePollStarted          = producerruntime.EventYouTubePollStarted
	EventYouTubePollSucceeded        = producerruntime.EventYouTubePollSucceeded
	EventYouTubePollFailed           = producerruntime.EventYouTubePollFailed
	EventYouTubeOutboxWriteStarted   = producerruntime.EventYouTubeOutboxWriteStarted
	EventYouTubeOutboxWriteSucceeded = producerruntime.EventYouTubeOutboxWriteSucceeded
	EventYouTubeOutboxWriteFailed    = producerruntime.EventYouTubeOutboxWriteFailed
	EventPhotoSyncStarted            = producerruntime.EventPhotoSyncStarted
	EventPhotoSyncSucceeded          = producerruntime.EventPhotoSyncSucceeded
	EventPhotoSyncFailed             = producerruntime.EventPhotoSyncFailed
)

var BuildYouTubeProducerRuntime = producerruntime.BuildYouTubeProducerRuntime
