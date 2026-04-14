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

const cacheScanBatchSize int64 = 100

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

	if err := clearSubscriberCacheNamespace(ctx, cacheSvc); err != nil {
		return CacheWarmSummary{}, err
	}

	return WarmSubscriberCacheFromAlarms(ctx, cacheSvc, alarms)
}

func clearSubscriberCacheNamespace(ctx context.Context, cacheSvc cache.Client) error {
	if cacheSvc == nil {
		return errors.New("rebuild subscriber cache from alarms: cache service is nil")
	}

	keysToDelete := []string{
		sharedalarmkeys.AlarmRegistryKey,
		sharedalarmkeys.AlarmChannelRegistryKey,
		sharedalarmkeys.MemberNameKey,
		sharedalarmkeys.RoomNamesCacheKey,
		sharedalarmkeys.UserNamesCacheKey,
	}

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	if err != nil {
		return fmt.Errorf("rebuild subscriber cache from alarms: read room registry: %w", err)
	}
	for _, roomID := range registryRooms {
		roomID = strings.TrimSpace(roomID)
		if roomID == "" {
			continue
		}
		keysToDelete = append(keysToDelete, sharedalarmkeys.BuildRoomAlarmKey(roomID))
	}

	for _, pattern := range []string{
		sharedalarmkeys.ChannelSubscribersKeyPrefix + "*",
		sharedalarmkeys.ChannelSubscribersEmptyKeyPrefix + "*",
	} {
		keys, scanErr := cacheSvc.ScanKeys(ctx, pattern, cacheScanBatchSize)
		if scanErr != nil {
			return fmt.Errorf("rebuild subscriber cache from alarms: scan keys %q: %w", pattern, scanErr)
		}
		keysToDelete = append(keysToDelete, keys...)
	}

	keysToDelete = compactUniqueStrings(keysToDelete)
	if len(keysToDelete) == 0 {
		return nil
	}

	if _, err := cacheSvc.DelMany(ctx, keysToDelete); err != nil {
		return fmt.Errorf("rebuild subscriber cache from alarms: delete existing keys: %w", err)
	}

	return nil
}

func compactUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func WarmSubscriberCacheFromAlarms(ctx context.Context, cacheSvc cache.Client, alarms []*domain.Alarm) (CacheWarmSummary, error) {
	if cacheSvc == nil {
		return CacheWarmSummary{}, errors.New("warm subscriber cache from alarms: cache service is nil")
	}

	summary := CacheWarmSummary{}
	rooms := make(map[string]struct{}, len(alarms))
	channels := make(map[string]struct{}, len(alarms))
	roomAlarmMembers := make(map[string][]string, len(alarms))
	channelSubscribers := make(map[string][]string, len(alarms))
	memberNames := make(map[string]string, len(alarms))
	roomNames := make(map[string]string, len(alarms))
	userNames := make(map[string]string, len(alarms))
	registryRooms := make([]string, 0, len(alarms))
	channelRegistry := make([]string, 0, len(alarms))

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

		registryKey := alarmRecord.RegistryKey()
		roomAlarmMembers[sharedalarmkeys.BuildRoomAlarmKey(roomID)] = append(
			roomAlarmMembers[sharedalarmkeys.BuildRoomAlarmKey(roomID)],
			channelID,
		)
		registryRooms = append(registryRooms, registryKey)
		channelRegistry = append(channelRegistry, channelID)

		for _, alarmType := range alarmTypes {
			key := sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)
			channelSubscribers[key] = append(channelSubscribers[key], registryKey)
		}

		if alarmRecord.MemberName != "" {
			memberNames[channelID] = alarmRecord.MemberName
		}
		if alarmRecord.RoomName != "" {
			roomNames[roomID] = alarmRecord.RoomName
		}
		if alarmRecord.UserName != "" && alarmRecord.UserID != "" {
			userNames[alarmRecord.UserID] = alarmRecord.UserName
		}

		summary.AlarmCount++
		rooms[roomID] = struct{}{}
		channels[channelID] = struct{}{}
	}

	for key, members := range roomAlarmMembers {
		dedupedMembers := compactUniqueStrings(members)
		if len(dedupedMembers) == 0 {
			continue
		}

		if _, err := cacheSvc.SAdd(ctx, key, dedupedMembers); err != nil {
			return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: add room alarms for key %s: %w", key, err)
		}
	}

	registryRooms = compactUniqueStrings(registryRooms)
	if len(registryRooms) > 0 {
		if _, err := cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmRegistryKey, registryRooms); err != nil {
			return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: add room registry: %w", err)
		}
	}

	channelRegistry = compactUniqueStrings(channelRegistry)
	if len(channelRegistry) > 0 {
		if _, err := cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, channelRegistry); err != nil {
			return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: add channel registry: %w", err)
		}
	}

	for key, members := range channelSubscribers {
		dedupedMembers := compactUniqueStrings(members)
		if len(dedupedMembers) == 0 {
			continue
		}

		if _, err := cacheSvc.SAdd(ctx, key, dedupedMembers); err != nil {
			return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: add channel subscribers for key %s: %w", key, err)
		}
	}

	if err := writeWarmHash(ctx, cacheSvc, sharedalarmkeys.MemberNameKey, memberNames); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: cache member names: %w", err)
	}

	if err := writeWarmHash(ctx, cacheSvc, sharedalarmkeys.RoomNamesCacheKey, roomNames); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: cache room names: %w", err)
	}

	if err := writeWarmHash(ctx, cacheSvc, sharedalarmkeys.UserNamesCacheKey, userNames); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: cache user names: %w", err)
	}

	summary.RoomCount = len(rooms)
	summary.ChannelCount = len(channels)

	return summary, nil
}

func writeWarmHash(ctx context.Context, cacheSvc cache.Client, key string, values map[string]string) (err error) {
	if len(values) == 0 {
		return nil
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = nil
			for field, value := range values {
				if setErr := cacheSvc.HSet(ctx, key, field, value); setErr != nil {
					err = setErr
					return
				}
			}
		}
	}()

	fields := make(map[string]any, len(values))
	for field, value := range values {
		fields[field] = value
	}
	if err := cacheSvc.HMSet(ctx, key, fields); err == nil {
		return nil
	}

	for field, value := range values {
		if setErr := cacheSvc.HSet(ctx, key, field, value); setErr != nil {
			return setErr
		}
	}
	return nil
}
