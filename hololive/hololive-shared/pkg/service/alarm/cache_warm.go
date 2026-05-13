package alarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

	keysToDelete, err := subscriberCacheStaticKeysToDelete(ctx, cacheSvc)
	if err != nil {
		return err
	}

	roomAlarmKeys, err := scanRoomAlarmKeys(ctx, cacheSvc)
	if err != nil {
		return err
	}
	keysToDelete = append(keysToDelete, roomAlarmKeys...)

	patternKeys, err := scanSubscriberCachePatternKeys(ctx, cacheSvc)
	if err != nil {
		return err
	}
	keysToDelete = append(keysToDelete, patternKeys...)

	return deleteSubscriberCacheKeys(ctx, cacheSvc, keysToDelete)
}

func subscriberCacheStaticKeysToDelete(ctx context.Context, cacheSvc cache.Client) ([]string, error) {
	keysToDelete := []string{
		sharedalarmkeys.AlarmRegistryKey,
		sharedalarmkeys.AlarmChannelRegistryKey,
		sharedalarmkeys.AlarmChannelRegistryVersionKey,
		sharedalarmkeys.AlarmSubscriberCacheEmptyKey,
		sharedalarmkeys.MemberNameKey,
		sharedalarmkeys.RoomNamesCacheKey,
		sharedalarmkeys.UserNamesCacheKey,
	}

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	if err != nil {
		return nil, fmt.Errorf("rebuild subscriber cache from alarms: read room registry: %w", err)
	}
	for _, roomID := range registryRooms {
		keysToDelete = appendRoomAlarmKey(keysToDelete, roomID)
	}
	return keysToDelete, nil
}

func appendRoomAlarmKey(keys []string, roomID string) []string {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return keys
	}
	return append(keys, sharedalarmkeys.BuildRoomAlarmKey(roomID))
}

func scanSubscriberCachePatternKeys(ctx context.Context, cacheSvc cache.Client) ([]string, error) {
	var keysToDelete []string
	for _, pattern := range []string{
		sharedalarmkeys.ChannelSubscribersKeyPrefix + "*",
		sharedalarmkeys.ChannelSubscribersEmptyKeyPrefix + "*",
	} {
		keys, scanErr := cacheSvc.ScanKeys(ctx, pattern, cacheScanBatchSize)
		if scanErr != nil {
			return nil, fmt.Errorf("rebuild subscriber cache from alarms: scan keys %q: %w", pattern, scanErr)
		}
		keysToDelete = append(keysToDelete, keys...)
	}
	return keysToDelete, nil
}

func deleteSubscriberCacheKeys(ctx context.Context, cacheSvc cache.Client, keysToDelete []string) error {
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
		if sharedalarmkeys.IsRoomAlarmKey(key) {
			roomKeys = append(roomKeys, key)
		}
	}
	return roomKeys, nil
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

	warmData := newSubscriberCacheWarmData(alarms)
	for _, alarmRecord := range alarms {
		warmData.addAlarm(alarmRecord)
	}

	if err := writeSubscriberCacheWarmData(ctx, cacheSvc, warmData); err != nil {
		return CacheWarmSummary{}, err
	}

	return warmData.finish(), nil
}

func normalizedWarmAlarmIdentity(alarmRecord *domain.Alarm) (string, string, bool) {
	if alarmRecord == nil {
		return "", "", false
	}
	roomID := strings.TrimSpace(alarmRecord.RoomID)
	channelID := strings.TrimSpace(alarmRecord.ChannelID)
	return roomID, channelID, roomID != "" && channelID != ""
}

func writeSubscriberCacheWarmData(ctx context.Context, cacheSvc cache.Client, data *subscriberCacheWarmData) error {
	if err := writeSubscriberCacheSets(ctx, cacheSvc, data); err != nil {
		return err
	}
	if err := writeSubscriberCacheHashes(ctx, cacheSvc, data); err != nil {
		return err
	}
	return writeSubscriberCacheWarmMarkers(ctx, cacheSvc, data.summary.AlarmCount == 0)
}

func writeSubscriberCacheSets(ctx context.Context, cacheSvc cache.Client, data *subscriberCacheWarmData) error {
	if err := writeWarmSetMap(ctx, cacheSvc, data.roomAlarmMembers, "room alarms"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}
	if err := writeWarmSet(ctx, cacheSvc, sharedalarmkeys.AlarmRegistryKey, compactUniqueStrings(data.registryRooms), "room registry"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}
	if err := writeWarmSet(ctx, cacheSvc, sharedalarmkeys.AlarmChannelRegistryKey, compactUniqueStrings(data.channelRegistry), "channel registry"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}
	if err := writeWarmSetMap(ctx, cacheSvc, data.channelSubscribers, "channel subscribers"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}
	return nil
}

func writeSubscriberCacheHashes(ctx context.Context, cacheSvc cache.Client, data *subscriberCacheWarmData) error {
	if err := writeWarmHash(ctx, cacheSvc, sharedalarmkeys.MemberNameKey, data.memberNames); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: cache member names: %w", err)
	}
	if err := writeWarmHash(ctx, cacheSvc, sharedalarmkeys.RoomNamesCacheKey, data.roomNames); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: cache room names: %w", err)
	}
	if err := writeWarmHash(ctx, cacheSvc, sharedalarmkeys.UserNamesCacheKey, data.userNames); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: cache user names: %w", err)
	}
	return nil
}

func writeSubscriberCacheWarmMarkers(ctx context.Context, cacheSvc cache.Client, empty bool) error {
	if err := markSubscriberCacheEmptyState(ctx, cacheSvc, empty); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: mark empty state: %w", err)
	}
	if err := bumpAlarmChannelRegistryVersion(ctx, cacheSvc); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: bump channel registry version: %w", err)
	}
	return nil
}

func bumpAlarmChannelRegistryVersion(ctx context.Context, cacheSvc cache.Client) error {
	return cacheSvc.Set(ctx, sharedalarmkeys.AlarmChannelRegistryVersionKey, time.Now().UTC().UnixNano(), 0)
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
		return writeWarmSetMapSequential(ctx, cacheSvc, setMembers, scope)
	}

	return writeWarmSetMapBatch(ctx, cacheSvc, setMembers, scope)
}

func writeWarmSetMapSequential(ctx context.Context, cacheSvc cache.Client, setMembers map[string][]string, scope string) error {
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

func writeWarmSetMapBatch(ctx context.Context, cacheSvc cache.Client, setMembers map[string][]string, scope string) error {
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
	return verifyWarmSetBatchResults(results, keys, scope)
}

func verifyWarmSetBatchResults(results []valkey.ValkeyResult, keys []string, scope string) error {
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
			err = writeWarmHashFields(ctx, cacheSvc, key, values)
		}
	}()

	fields := make(map[string]any, len(values))
	for field, value := range values {
		fields[field] = value
	}
	if err := cacheSvc.HMSet(ctx, key, fields); err == nil {
		return nil
	}

	return writeWarmHashFields(ctx, cacheSvc, key, values)
}

func writeWarmHashFields(ctx context.Context, cacheSvc cache.Client, key string, values map[string]string) error {
	for field, value := range values {
		if setErr := cacheSvc.HSet(ctx, key, field, value); setErr != nil {
			return setErr
		}
	}
	return nil
}
