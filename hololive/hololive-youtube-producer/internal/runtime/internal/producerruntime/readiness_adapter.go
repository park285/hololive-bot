package producerruntime

import "github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"

func newReadinessState(features ingestionRuntimeFeatures) *readiness.State {
	return newReadinessStateWithFetcherEngine(youtubeProducerRuntimeName, features, "")
}

func newReadinessStateWithFetcherEngine(runtimeName string, features ingestionRuntimeFeatures, scraperFetcherEngine string) *readiness.State {
	return readiness.New(runtimeName, readiness.Features{
		YouTubeEnabled:       features.youtubeEnabled,
		PhotoSyncEnabled:     features.photoSyncEnabled,
		ActiveActiveEnabled:  features.activeActiveEnabled,
		ActiveActiveInstance: features.activeActiveInstanceID,
		ScraperFetcherEngine: scraperFetcherEngine,
	})
}
