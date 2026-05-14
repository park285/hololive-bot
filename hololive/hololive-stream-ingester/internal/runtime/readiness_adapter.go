package runtime

import "github.com/kapu/hololive-stream-ingester/internal/runtime/readiness"

func newReadinessState(runtimeName string, features ingestionRuntimeFeatures) *readiness.State {
	return readiness.New(runtimeName, readiness.Features{
		YouTubeEnabled:   features.youtubeEnabled,
		PhotoSyncEnabled: features.photoSyncEnabled,
	})
}
