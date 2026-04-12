package alarm

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type CacheWarmSummary struct {
	AlarmCount   int
	RoomCount    int
	ChannelCount int
	KeysDeleted  int
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

func RebuildSubscriberCacheFromRepository(ctx context.Context, cacheSvc cache.Client, repo *Repository) (CacheWarmSummary, error) {
	if repo == nil {
		return CacheWarmSummary{}, errors.New("rebuild subscriber cache from repository: repository is nil")
	}
	if cacheSvc == nil {
		return CacheWarmSummary{}, errors.New("rebuild subscriber cache from repository: cache service is nil")
	}

	return rebuildSubscriberCacheFromLoader(ctx, cacheSvc, func(ctx context.Context) ([]*domain.Alarm, error) {
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

func rebuildSubscriberCacheFromLoader(ctx context.Context, cacheSvc cache.Client, load alarmLoader) (CacheWarmSummary, error) {
	alarms, err := load(ctx)
	if err != nil {
		return CacheWarmSummary{}, fmt.Errorf("rebuild subscriber cache from repository: load alarms: %w", err)
	}

	deletedCount, err := resetSubscriberCacheKeys(ctx, cacheSvc)
	if err != nil {
		return CacheWarmSummary{}, fmt.Errorf("rebuild subscriber cache from repository: reset cache keys: %w", err)
	}

	summary, err := WarmSubscriberCacheFromAlarms(ctx, cacheSvc, alarms)
	if err != nil {
		return CacheWarmSummary{}, err
	}
	summary.KeysDeleted = deletedCount
	return summary, nil
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

func resetSubscriberCacheKeys(ctx context.Context, cacheSvc cache.Client) (int, error) {
	if cacheSvc == nil {
		return 0, errors.New("reset subscriber cache keys: cache service is nil")
	}

	keysToDelete := map[string]struct{}{
		sharedalarmkeys.AlarmRegistryKey:        {},
		sharedalarmkeys.AlarmChannelRegistryKey: {},
		sharedalarmkeys.MemberNameKey:           {},
		sharedalarmkeys.RoomNamesCacheKey:       {},
		sharedalarmkeys.UserNamesCacheKey:       {},
	}

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	if err != nil {
		return 0, fmt.Errorf("load alarm registry rooms: %w", err)
	}
	for _, roomID := range registryRooms {
		roomID = strings.TrimSpace(roomID)
		if roomID == "" {
			continue
		}
		keysToDelete[sharedalarmkeys.BuildRoomAlarmKey(roomID)] = struct{}{}
	}

	channelIDs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	if err != nil {
		return 0, fmt.Errorf("load alarm channel registry: %w", err)
	}
	for _, channelID := range channelIDs {
		channelID = strings.TrimSpace(channelID)
		if channelID == "" {
			continue
		}
		for _, alarmType := range domain.AllAlarmTypes {
			keysToDelete[sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)] = struct{}{}
			keysToDelete[sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType)] = struct{}{}
		}
	}

	roomKeys, err := cacheSvc.ScanKeys(ctx, sharedalarmkeys.AlarmKeyPrefix+"*", 500)
	if err != nil {
		return 0, fmt.Errorf("scan room alarm keys: %w", err)
	}
	for _, key := range roomKeys {
		key = strings.TrimSpace(key)
		if key == "" || !isRoomAlarmCacheKey(key) {
			continue
		}
		keysToDelete[key] = struct{}{}
	}

	subscriberKeys, err := cacheSvc.ScanKeys(ctx, sharedalarmkeys.ChannelSubscribersKeyPrefix+"*", 500)
	if err != nil {
		return 0, fmt.Errorf("scan subscriber keys: %w", err)
	}
	for _, key := range subscriberKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keysToDelete[key] = struct{}{}
	}

	emptyKeys, err := cacheSvc.ScanKeys(ctx, sharedalarmkeys.ChannelSubscribersEmptyKeyPrefix+"*", 500)
	if err != nil {
		return 0, fmt.Errorf("scan empty subscriber keys: %w", err)
	}
	for _, key := range emptyKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keysToDelete[key] = struct{}{}
	}

	deleteList := make([]string, 0, len(keysToDelete))
	for key := range keysToDelete {
		deleteList = append(deleteList, key)
	}
	if len(deleteList) == 0 {
		return 0, nil
	}
	sort.Strings(deleteList)

	deletedCount, err := cacheSvc.DelMany(ctx, deleteList)
	if err != nil {
		return 0, fmt.Errorf("delete subscriber cache keys: %w", err)
	}

	return int(deletedCount), nil
}

func isRoomAlarmCacheKey(key string) bool {
	if !strings.HasPrefix(key, sharedalarmkeys.AlarmKeyPrefix) {
		return false
	}

	switch key {
	case sharedalarmkeys.AlarmRegistryKey,
		sharedalarmkeys.AlarmChannelRegistryKey,
		sharedalarmkeys.MemberNameKey,
		sharedalarmkeys.RoomNamesCacheKey,
		sharedalarmkeys.UserNamesCacheKey:
		return false
	}

	if strings.HasPrefix(key, sharedalarmkeys.ChannelSubscribersKeyPrefix) {
		return false
	}
	if strings.HasPrefix(key, sharedalarmkeys.ChannelSubscribersEmptyKeyPrefix) {
		return false
	}

	return true
}
