package producerruntime

import "github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"

func newReadinessState(runtimeName string, features ingestionRuntimeFeatures) *readiness.State {
	return readiness.New(runtimeName, readiness.Features{
		YouTubeEnabled:       features.youtubeEnabled,
		PhotoSyncEnabled:     features.photoSyncEnabled,
		ActiveActiveEnabled:  features.activeActiveEnabled,
		ActiveActiveInstance: features.activeActiveInstanceID,
	})
}
