package runtime

import (
	"context"
	"fmt"
	"log/slog"

	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

var rebuildSubscriberCacheFromRepository = sharedalarm.RebuildSubscriberCacheFromRepository

type subscriberCacheWarmResult struct {
	Summary sharedalarm.CacheWarmSummary
	Rebuilt bool
}

func warmSubscriberCacheFromDBIfCacheCold(
	ctx context.Context,
	cacheService cache.Client,
	postgresService database.Client,
	logger *slog.Logger,
) (subscriberCacheWarmResult, error) {
	if cacheService == nil {
		return subscriberCacheWarmResult{}, fmt.Errorf("warm subscriber cache from db if cache cold: cache service is nil")
	}

	channelIDs, err := cacheService.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	if err != nil {
		return subscriberCacheWarmResult{}, fmt.Errorf("warm subscriber cache from db if cache cold: read channel registry: %w", err)
	}
	if len(channelIDs) > 0 {
		if logger != nil {
			logger.Info("subscriber_cache_rebuild_skipped",
				slog.Int("existing_channel_registry_count", len(channelIDs)),
			)
		}
		return subscriberCacheWarmResult{}, nil
	}

	repo := sharedalarm.NewRepository(postgresService, logger)

	summary, err := rebuildSubscriberCacheFromRepository(ctx, cacheService, repo)
	if err != nil {
		return subscriberCacheWarmResult{}, fmt.Errorf("warm subscriber cache from db if cache cold: %w", err)
	}

	if logger == nil {
		return subscriberCacheWarmResult{Summary: summary, Rebuilt: true}, nil
	}
	logger.Info("subscriber_cache_rebuilt_from_db",
		slog.Int("alarms_loaded", summary.AlarmCount),
		slog.Int("rooms_loaded", summary.RoomCount),
		slog.Int("channels_loaded", summary.ChannelCount),
		slog.Int("keys_deleted", summary.KeysDeleted),
	)

	return subscriberCacheWarmResult{Summary: summary, Rebuilt: true}, nil
}
