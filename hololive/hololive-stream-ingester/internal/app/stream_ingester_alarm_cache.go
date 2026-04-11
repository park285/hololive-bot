package app

import (
	"context"
	"fmt"
	"log/slog"

	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

func warmSubscriberCacheFromDB(ctx context.Context, cacheService cache.Client, postgresService database.Client, logger *slog.Logger) error {
	repo := sharedalarm.NewRepository(postgresService, logger)

	summary, err := sharedalarm.WarmSubscriberCacheFromRepository(ctx, cacheService, repo)
	if err != nil {
		return fmt.Errorf("warm subscriber cache from db: %w", err)
	}

	if logger == nil {
		return nil
	}

	if summary.AlarmCount == 0 {
		logger.Info("No alarms found in DB, subscriber cache warming skipped")
		return nil
	}

	logger.Info("Subscriber cache warmed from DB",
		slog.Int("alarms_loaded", summary.AlarmCount),
		slog.Int("rooms_loaded", summary.RoomCount),
		slog.Int("channels_loaded", summary.ChannelCount),
	)

	return nil
}
