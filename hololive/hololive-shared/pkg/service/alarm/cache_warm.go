package alarm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type CacheWarmSummary struct {
	AlarmCount   int
	RoomCount    int
	ChannelCount int
}

type alarmLoader func(context.Context) ([]*domain.Alarm, error)

var loadAllAlarmsFromRepository = func(ctx context.Context, repo *Repository) ([]*domain.Alarm, error) {
	return repo.LoadAll(ctx)
}

func WarmSubscriberCacheFromRepository(ctx context.Context, cacheSvc cache.Client, repo *Repository) (CacheWarmSummary, error) {
	if repo == nil {
		return CacheWarmSummary{}, errors.New("warm subscriber cache from repository: repository is nil")
	}

	return warmSubscriberCacheFromLoader(ctx, cacheSvc, func(ctx context.Context) ([]*domain.Alarm, error) {
		return loadAllAlarmsFromRepository(ctx, repo)
	})
}

func warmSubscriberCacheFromLoader(ctx context.Context, cacheSvc cache.Client, load alarmLoader) (CacheWarmSummary, error) {
	alarms, err := load(ctx)
	if err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from repository: load alarms: %w", err)
	}

	return WarmSubscriberCacheFromAlarms(ctx, cacheSvc, alarms)
}

func WarmSubscriberCacheFromAlarms(ctx context.Context, cacheSvc cache.Client, alarms []*domain.Alarm) (CacheWarmSummary, error) {
	if cacheSvc == nil {
		return CacheWarmSummary{}, errors.New("warm subscriber cache from alarms: cache service is nil")
	}

	summary := CacheWarmSummary{}
	rooms := make(map[string]struct{}, len(alarms))
	channels := make(map[string]struct{}, len(alarms))

	for _, alarmRecord := range alarms {
		if alarmRecord == nil {
			continue
		}

		roomID := strings.TrimSpace(alarmRecord.RoomID)
		channelID := strings.TrimSpace(alarmRecord.ChannelID)
		if roomID == "" || channelID == "" {
			continue
		}

		alarmTypes := alarmRecord.AlarmTypes
		if len(alarmTypes) == 0 {
			alarmTypes = domain.DefaultAlarmTypes
		}

		if _, err := cacheSvc.SAdd(ctx, sharedalarmkeys.BuildRoomAlarmKey(roomID), []string{channelID}); err != nil {
			return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: add room alarm key for room %s channel %s: %w", roomID, channelID, err)
		}
		if _, err := cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, []string{alarmRecord.RegistryKey()}); err != nil {
			return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: add room registry for room %s: %w", roomID, err)
		}
		if _, err := cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{channelID}); err != nil {
			return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: add channel registry for channel %s: %w", channelID, err)
		}

		for _, alarmType := range alarmTypes {
			key := sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)
			if _, err := cacheSvc.SAdd(ctx, key, []string{alarmRecord.RegistryKey()}); err != nil {
				return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: add subscriber key for room %s channel %s type %s: %w", roomID, channelID, alarmType, err)
			}
		}

		if alarmRecord.MemberName != "" {
			if err := cacheSvc.HSet(ctx, sharedalarmkeys.MemberNameKey, channelID, alarmRecord.MemberName); err != nil {
				return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: cache member name for channel %s: %w", channelID, err)
			}
		}
		if alarmRecord.RoomName != "" {
			if err := cacheSvc.HSet(ctx, sharedalarmkeys.RoomNamesCacheKey, roomID, alarmRecord.RoomName); err != nil {
				return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: cache room name for room %s: %w", roomID, err)
			}
		}
		if alarmRecord.UserName != "" && alarmRecord.UserID != "" {
			if err := cacheSvc.HSet(ctx, sharedalarmkeys.UserNamesCacheKey, alarmRecord.UserID, alarmRecord.UserName); err != nil {
				return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: cache user name for room %s: %w", roomID, err)
			}
		}

		summary.AlarmCount++
		rooms[roomID] = struct{}{}
		channels[channelID] = struct{}{}
	}

	summary.RoomCount = len(rooms)
	summary.ChannelCount = len(channels)

	return summary, nil
}
