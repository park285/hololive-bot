package runtime

import ingesterruntime "github.com/kapu/hololive-stream-ingester/internal/runtime/internal/ingesterruntime"

type StreamIngesterRuntime = ingesterruntime.StreamIngesterRuntime

const (
	EventIngestionRuntimeConfigured  = ingesterruntime.EventIngestionRuntimeConfigured
	EventIngestionRuntimeStarted     = ingesterruntime.EventIngestionRuntimeStarted
	EventIngestionRuntimeStopped     = ingesterruntime.EventIngestionRuntimeStopped
	EventYouTubePollStarted          = ingesterruntime.EventYouTubePollStarted
	EventYouTubePollSucceeded        = ingesterruntime.EventYouTubePollSucceeded
	EventYouTubePollFailed           = ingesterruntime.EventYouTubePollFailed
	EventYouTubeOutboxWriteStarted   = ingesterruntime.EventYouTubeOutboxWriteStarted
	EventYouTubeOutboxWriteSucceeded = ingesterruntime.EventYouTubeOutboxWriteSucceeded
	EventYouTubeOutboxWriteFailed    = ingesterruntime.EventYouTubeOutboxWriteFailed
	EventPhotoSyncStarted            = ingesterruntime.EventPhotoSyncStarted
	EventPhotoSyncSucceeded          = ingesterruntime.EventPhotoSyncSucceeded
	EventPhotoSyncFailed             = ingesterruntime.EventPhotoSyncFailed
)

var BuildStreamIngesterRuntime = ingesterruntime.BuildStreamIngesterRuntime
var BuildYouTubeScraperRuntime = ingesterruntime.BuildYouTubeScraperRuntime
