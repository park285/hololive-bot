package alarm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/valkey-io/valkey-go"
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
		sharedalarmkeys.AlarmSubscriberCacheEmptyKey,
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

	roomAlarmKeys, err := scanRoomAlarmKeys(ctx, cacheSvc)
	if err != nil {
		return err
	}
	keysToDelete = append(keysToDelete, roomAlarmKeys...)

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

func scanRoomAlarmKeys(ctx context.Context, cacheSvc cache.Client) ([]string, error) {
	keys, err := cacheSvc.ScanKeys(ctx, sharedalarmkeys.AlarmKeyPrefix+"*", cacheScanBatchSize)
	if err != nil {
		return nil, fmt.Errorf("rebuild subscriber cache from alarms: scan room alarm keys: %w", err)
	}

	roomKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if isRoomAlarmCacheKey(key) {
			roomKeys = append(roomKeys, key)
		}
	}
	return roomKeys, nil
}

func isRoomAlarmCacheKey(key string) bool {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || !strings.HasPrefix(trimmed, sharedalarmkeys.AlarmKeyPrefix) {
		return false
	}

	switch trimmed {
	case sharedalarmkeys.AlarmRegistryKey,
		sharedalarmkeys.AlarmChannelRegistryKey,
		sharedalarmkeys.AlarmSubscriberCacheEmptyKey,
		sharedalarmkeys.MemberNameKey,
		sharedalarmkeys.RoomNamesCacheKey,
		sharedalarmkeys.UserNamesCacheKey,
		contractsalarm.DispatchQueueKey,
		contractsalarm.DispatchRetryQueueKey,
		contractsalarm.DispatchDLQKey,
		"alarm:chzzk_channels",
		"alarm:twitch_logins",
		"alarm:twitch_channel_logins":
		return false
	}

	suffix := strings.TrimPrefix(trimmed, sharedalarmkeys.AlarmKeyPrefix)
	if suffix == "" {
		return false
	}

	// 실제 room alarm cache key 는 alarm:{roomID} 단일 세그먼트 형태를 따른다.
	// 추가 ':' 가 있는 키는 dispatch / typed subscriber / next_stream 등 다른 alarm namespace 로 본다.
	if strings.Contains(suffix, ":") {
		return false
	}

	return true
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

	if err := writeWarmSetMap(ctx, cacheSvc, roomAlarmMembers, "room alarms"); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}

	registryRooms = compactUniqueStrings(registryRooms)
	if err := writeWarmSet(ctx, cacheSvc, sharedalarmkeys.AlarmRegistryKey, registryRooms, "room registry"); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}

	channelRegistry = compactUniqueStrings(channelRegistry)
	if err := writeWarmSet(ctx, cacheSvc, sharedalarmkeys.AlarmChannelRegistryKey, channelRegistry, "channel registry"); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}

	if err := writeWarmSetMap(ctx, cacheSvc, channelSubscribers, "channel subscribers"); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: %w", err)
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
	if err := markSubscriberCacheEmptyState(ctx, cacheSvc, summary.AlarmCount == 0); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("warm subscriber cache from alarms: mark empty state: %w", err)
	}

	return summary, nil
}

func markSubscriberCacheEmptyState(ctx context.Context, cacheSvc cache.Client, empty bool) error {
	if empty {
		return cacheSvc.Set(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey, "1", 0)
	}

	return cacheSvc.Del(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
}

func writeWarmSet(ctx context.Context, cacheSvc cache.Client, key string, members []string, scope string) error {
	return writeWarmSetMap(ctx, cacheSvc, map[string][]string{key: members}, scope)
}

func writeWarmSetMap(ctx context.Context, cacheSvc cache.Client, setMembers map[string][]string, scope string) error {
	if len(setMembers) == 0 {
		return nil
	}

	if !supportsWarmSetBatch(cacheSvc) {
		for key, members := range setMembers {
			dedupedMembers := compactUniqueStrings(members)
			if len(dedupedMembers) == 0 {
				continue
			}
			if _, err := cacheSvc.SAdd(ctx, key, dedupedMembers); err != nil {
				return fmt.Errorf("add %s for key %s: %w", scope, key, err)
			}
		}
		return nil
	}

	keys := make([]string, 0, len(setMembers))
	cmds := make([]valkey.Completed, 0, len(setMembers))
	for key, members := range setMembers {
		dedupedMembers := compactUniqueStrings(members)
		if len(dedupedMembers) == 0 {
			continue
		}
		keys = append(keys, key)
		cmds = append(cmds, cacheSvc.Builder().Sadd().Key(key).Member(dedupedMembers...).Build())
	}
	if len(cmds) == 0 {
		return nil
	}

	results := cacheSvc.DoMulti(ctx, cmds...)
	if len(results) != len(cmds) {
		return fmt.Errorf("add %s: unexpected result count: %d", scope, len(results))
	}
	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("add %s for key %s: %w", scope, keys[i], err)
		}
	}
	return nil
}

func supportsWarmSetBatch(cacheSvc cache.Client) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	builder := cacheSvc.Builder()
	return builder != (valkey.Builder{})
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
