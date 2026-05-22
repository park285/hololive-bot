package producerruntime

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
)

type ingestionRuntimeFeatures struct {
	youtubeEnabled                bool
	photoSyncEnabled              bool
	communityShortsBigBangEnabled bool
	activeActiveEnabled           bool
	activeActiveInstanceID        string
}

type ingestionRuntimeSpec struct {
	name              string
	requestedFeatures ingestionRuntimeFeatures
	features          ingestionRuntimeFeatures
}

func youtubeProducerSpec(appConfig *config.Config) ingestionRuntimeSpec {
	requested := requestedFeatures(appConfig)

	return ingestionRuntimeSpec{
		name:              youtubeProducerRuntimeName,
		requestedFeatures: requested,
		features: ingestionRuntimeFeatures{
			youtubeEnabled:                requested.youtubeEnabled,
			photoSyncEnabled:              requested.photoSyncEnabled,
			communityShortsBigBangEnabled: requested.communityShortsBigBangEnabled,
			activeActiveEnabled:           requested.activeActiveEnabled,
			activeActiveInstanceID:        requested.activeActiveInstanceID,
		},
	}
}

func requestedFeatures(appConfig *config.Config) ingestionRuntimeFeatures {
	if appConfig == nil {
		return ingestionRuntimeFeatures{}
	}

	return ingestionRuntimeFeatures{
		youtubeEnabled:                appConfig.Ingestion.YouTubeEnabled,
		photoSyncEnabled:              appConfig.Ingestion.PhotoSyncEnabled,
		communityShortsBigBangEnabled: appConfig.Ingestion.CommunityShortsBigBangEnabled,
		activeActiveEnabled:           appConfig.Scraper.ActiveActive.Enabled,
		activeActiveInstanceID:        appConfig.Scraper.ActiveActive.InstanceID,
	}
}

func logFeatureOverride(logger *slog.Logger, spec ingestionRuntimeSpec) {
	if logger == nil {
		return
	}
	if spec.requestedFeatures == spec.features {
		return
	}

	logger.Warn("YouTube producer runtime overrides ingestion feature toggles",
		slog.String("runtime", spec.name),
		slog.Bool("requested_youtube_enabled", spec.requestedFeatures.youtubeEnabled),
		slog.Bool("effective_youtube_enabled", spec.features.youtubeEnabled),
		slog.Bool("requested_photo_sync_enabled", spec.requestedFeatures.photoSyncEnabled),
		slog.Bool("effective_photo_sync_enabled", spec.features.photoSyncEnabled),
		slog.Bool("requested_community_shorts_bigbang_enabled", spec.requestedFeatures.communityShortsBigBangEnabled),
		slog.Bool("effective_community_shorts_bigbang_enabled", spec.features.communityShortsBigBangEnabled),
		slog.Bool("requested_active_active_enabled", spec.requestedFeatures.activeActiveEnabled),
		slog.Bool("effective_active_active_enabled", spec.features.activeActiveEnabled),
	)
}
