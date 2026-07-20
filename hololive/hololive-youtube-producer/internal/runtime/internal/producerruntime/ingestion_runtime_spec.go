package producerruntime

import "github.com/kapu/hololive-shared/pkg/config"

type ingestionRuntimeFeatures struct {
	youtubeEnabled         bool
	photoSyncEnabled       bool
	activeActiveEnabled    bool
	activeActiveInstanceID string
}

type ingestionRuntimeSpec struct {
	name     string
	features ingestionRuntimeFeatures
}

func youtubeProducerSpec(appConfig *config.Config) ingestionRuntimeSpec {
	return ingestionRuntimeSpec{
		name:     youtubeProducerRuntimeName,
		features: requestedFeatures(appConfig),
	}
}

func requestedFeatures(appConfig *config.Config) ingestionRuntimeFeatures {
	if appConfig == nil {
		return ingestionRuntimeFeatures{}
	}

	return ingestionRuntimeFeatures{
		youtubeEnabled:         appConfig.Ingestion.YouTubeEnabled,
		photoSyncEnabled:       appConfig.Ingestion.PhotoSyncEnabled,
		activeActiveEnabled:    appConfig.Scraper.ActiveActive.Enabled,
		activeActiveInstanceID: appConfig.Scraper.ActiveActive.InstanceID,
	}
}
