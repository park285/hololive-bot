package alarm

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const emptyChannelSubscriberCacheTTL = 30 * time.Second

func markEmptyChannelSubscriberCache(ctx context.Context, cacheClient cache.Client, channelID string, alarmType domain.AlarmType) {
	if cacheClient == nil {
		return
	}

	if err := cacheClient.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType), "1", emptyChannelSubscriberCacheTTL); err != nil {
		observeAlarmSubscriberCacheError("mark_empty")
	}
}

func warmChannelSubscriberCache(ctx context.Context, cacheClient cache.Client, alarms []*domain.Alarm, channelID string, alarmType domain.AlarmType) {
	if cacheClient == nil {
		return
	}

	subscribers := extractSubscriberIDsByType(alarms, alarmType)
	key := sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)

	if err := writeWarmSet(ctx, cacheClient, key, subscribers, "channel subscribers"); err != nil {
		observeAlarmSubscriberCacheError("warm")
	}

	if err := cacheClient.Del(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType)); err != nil {
		observeAlarmSubscriberCacheError("clear_empty")
	}
}
