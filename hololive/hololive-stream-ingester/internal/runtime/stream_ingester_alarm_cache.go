package runtime

import (
	"context"
	"fmt"
	"log/slog"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func observeSubscriberCacheOnYouTubeStartup(
	ctx context.Context,
	runtimeName string,
	youtubeEnabled bool,
	cacheService cache.Client,
	logger *slog.Logger,
) error {
	if !youtubeEnabled || cacheService == nil {
		return nil
	}

	channelIDs, err := cacheService.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	if err != nil {
		return fmt.Errorf("observe subscriber cache on youtube startup: read channel registry: %w", err)
	}

	if logger != nil {
		logger.Info("subscriber_cache_observed_on_youtube_startup",
			slog.String("runtime", runtimeName),
			slog.Int("existing_channel_registry_count", len(channelIDs)),
		)
	}

	return nil
}
