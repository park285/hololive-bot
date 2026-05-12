package runtime

import (
	"context"
	"log/slog"
	"time"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type youTubePollTargetResolver struct {
	cacheService        cache.Client
	loadAlarmChannelIDs func(context.Context) ([]string, error)
	logger              *slog.Logger
}

func (r *youTubePollTargetResolver) ResolveAlarmChannelIDs(
	ctx context.Context,
	now time.Time,
	lastNonEmptyCacheAt time.Time,
) (alarmChannelIDs []string, candidateFromCache bool, nextLastNonEmptyCacheAt time.Time, ok bool) {
	nextLastNonEmptyCacheAt = lastNonEmptyCacheAt
	cacheAlarmChannelIDs, cacheErr := r.cacheService.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	switch {
	case cacheErr == nil && len(cacheAlarmChannelIDs) > 0:
		return cacheAlarmChannelIDs, true, now, true
	case cacheErr == nil && len(cacheAlarmChannelIDs) == 0:
		if !lastNonEmptyCacheAt.IsZero() && now.Sub(lastNonEmptyCacheAt) < youtubePollTargetEmptyCacheGracePeriod {
			return cacheAlarmChannelIDs, true, nextLastNonEmptyCacheAt, true
		}
	default:
		if r.logger != nil {
			r.logger.Warn("Failed to refresh YouTube poll targets from cache",
				slog.Any("error", cacheErr))
		}
	}

	dbAlarmChannelIDs, dbErr := r.loadAlarmChannelIDs(ctx)
	if dbErr != nil {
		if r.logger != nil {
			r.logger.Warn("Failed to refresh YouTube poll targets from DB fallback",
				slog.Any("error", dbErr))
		}
		return nil, false, nextLastNonEmptyCacheAt, false
	}
	return dbAlarmChannelIDs, false, nextLastNonEmptyCacheAt, true
}
