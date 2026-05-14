package alarmcache

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func ObserveSubscriberCacheOnYouTubeStartup(
	ctx context.Context,
	runtimeName string,
	youtubeEnabled bool,
	cacheService cache.Client,
	logger *slog.Logger,
) error {
	return observeSubscriberCacheOnYouTubeStartup(ctx, runtimeName, youtubeEnabled, cacheService, logger)
}
