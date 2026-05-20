package alarmcache

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func ObserveSubscriberCacheOnProducerStartup(
	ctx context.Context,
	runtimeName string,
	youtubeEnabled bool,
	cacheService cache.Client,
	logger *slog.Logger,
) error {
	return observeSubscriberCacheOnProducerStartup(ctx, runtimeName, youtubeEnabled, cacheService, logger)
}
