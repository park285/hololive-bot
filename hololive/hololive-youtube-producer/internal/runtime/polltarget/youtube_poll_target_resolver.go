package polltarget

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
	if alarmChannelIDs, candidateFromCache, nextLastNonEmptyCacheAt, ok, resolved := resolveAlarmChannelIDsFromCache(
		cacheAlarmChannelIDs,
		cacheErr,
		now,
		lastNonEmptyCacheAt,
	); resolved {
		return alarmChannelIDs, candidateFromCache, nextLastNonEmptyCacheAt, ok
	}
	if cacheErr != nil {
		r.warnResolveAlarmChannelIDs("Failed to refresh YouTube poll targets from cache", cacheErr)
	}

	dbAlarmChannelIDs, dbErr := r.loadAlarmChannelIDs(ctx)
	if dbErr != nil {
		r.warnResolveAlarmChannelIDs("Failed to refresh YouTube poll targets from DB fallback", dbErr)
		return nil, false, nextLastNonEmptyCacheAt, false
	}
	return dbAlarmChannelIDs, false, nextLastNonEmptyCacheAt, true
}

func resolveAlarmChannelIDsFromCache(
	cacheAlarmChannelIDs []string,
	cacheErr error,
	now time.Time,
	lastNonEmptyCacheAt time.Time,
) (alarmChannelIDs []string, candidateFromCache bool, nextLastNonEmptyCacheAt time.Time, ok bool, resolved bool) {
	nextLastNonEmptyCacheAt = lastNonEmptyCacheAt
	if cacheErr != nil {
		return nil, false, nextLastNonEmptyCacheAt, false, false
	}
	if len(cacheAlarmChannelIDs) > 0 {
		return cacheAlarmChannelIDs, true, now, true, true
	}
	if isWithinYouTubePollTargetEmptyCacheGracePeriod(now, lastNonEmptyCacheAt) {
		return cacheAlarmChannelIDs, true, nextLastNonEmptyCacheAt, true, true
	}
	return nil, false, nextLastNonEmptyCacheAt, false, false
}

func isWithinYouTubePollTargetEmptyCacheGracePeriod(now time.Time, lastNonEmptyCacheAt time.Time) bool {
	return !lastNonEmptyCacheAt.IsZero() && now.Sub(lastNonEmptyCacheAt) < youtubePollTargetEmptyCacheGracePeriod
}

func (r *youTubePollTargetResolver) warnResolveAlarmChannelIDs(message string, err error) {
	if r.logger == nil {
		return
	}
	r.logger.Warn(message, slog.Any("error", err))
}
