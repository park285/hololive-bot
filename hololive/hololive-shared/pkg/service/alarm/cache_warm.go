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
)

type CacheWarmSummary struct {
	AlarmCount   int
	RoomCount    int
	ChannelCount int
}

var loadAllAlarmsFromRepository = func(ctx context.Context, repository *Repository) ([]*domain.Alarm, error) {
	return repository.LoadAll(ctx)
}

var loadMemberNamesFromRepository = func(ctx context.Context, repository *Repository) (map[string]string, error) {
	return repository.GetAllMemberNames(ctx)
}

const cacheScanBatchSize int64 = 100

func WarmSubscriberCacheFromRepository(ctx context.Context, cacheClient cache.Client, repository *Repository) (CacheWarmSummary, error) {
	if repository == nil {
		return CacheWarmSummary{}, errors.New("warm subscriber cache from repository: repository is nil")
	}

	return warmSubscriberCacheFromRepository(ctx, cacheClient, repository, false)
}

func RebuildSubscriberCacheFromRepository(ctx context.Context, cacheClient cache.Client, repository *Repository) (CacheWarmSummary, error) {
	if repository == nil {
		return CacheWarmSummary{}, errors.New("rebuild subscriber cache from repository: repository is nil")
	}

	return warmSubscriberCacheFromRepository(ctx, cacheClient, repository, true)
}

func warmSubscriberCacheFromRepository(ctx context.Context, cacheClient cache.Client, repository *Repository, rebuild bool) (CacheWarmSummary, error) {
	operation := subscriberCacheWarmOperation(rebuild)
	warmData, err := loadSubscriberCacheWarmData(ctx, repository, operation)
	if err != nil {
		return CacheWarmSummary{}, err
	}

	if rebuild {
		if err := clearSubscriberCacheNamespace(ctx, cacheClient); err != nil {
			return CacheWarmSummary{}, err
		}
	}

	if err := writeSubscriberCacheWarmData(ctx, cacheClient, warmData); err != nil {
		return CacheWarmSummary{}, err
	}

	return warmData.summary, nil
}

func subscriberCacheWarmOperation(rebuild bool) string {
	if rebuild {
		return "rebuild"
	}
	return "warm"
}

func loadSubscriberCacheWarmData(ctx context.Context, repository *Repository, operation string) (*subscriberCacheWarmData, error) {
	alarms, err := loadAllAlarmsFromRepository(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("%s subscriber cache from repository: load alarms: %w", operation, err)
	}

	warmData := newSubscriberCacheWarmData(alarms)
	for _, alarmRecord := range alarms {
		warmData.addAlarm(alarmRecord)
	}
	warmData.summary = warmData.finish()

	memberNames, err := loadMemberNamesFromRepository(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("%s subscriber cache from repository: load member names: %w", operation, err)
	}
	if len(memberNames) > 0 {
		warmData.memberNames = memberNames
	}

	return warmData, nil
}

func clearSubscriberCacheNamespace(ctx context.Context, cacheClient cache.Client) error {
	if cacheClient == nil {
		return errors.New("rebuild subscriber cache from alarms: cache service is nil")
	}

	keysToDelete, err := subscriberCacheStaticKeysToDelete(ctx, cacheClient)
	if err != nil {
		return err
	}

	roomAlarmKeys, err := scanRoomAlarmKeys(ctx, cacheClient)
	if err != nil {
		return err
	}
	keysToDelete = append(keysToDelete, roomAlarmKeys...)

	patternKeys, err := scanSubscriberCachePatternKeys(ctx, cacheClient)
	if err != nil {
		return err
	}
	keysToDelete = append(keysToDelete, patternKeys...)

	return deleteSubscriberCacheKeys(ctx, cacheClient, keysToDelete)
}

func subscriberCacheStaticKeysToDelete(ctx context.Context, cacheClient cache.Client) ([]string, error) {
	keysToDelete := []string{
		sharedalarmkeys.AlarmRegistryKey,
		sharedalarmkeys.AlarmChannelRegistryKey,
		sharedalarmkeys.AlarmChannelRegistryVersionKey,
		sharedalarmkeys.AlarmSubscriberCacheEmptyKey,
		sharedalarmkeys.MemberNameKey,
		sharedalarmkeys.RoomNamesCacheKey,
		sharedalarmkeys.UserNamesCacheKey,
	}

	registryRooms, err := cacheClient.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
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

func scanSubscriberCachePatternKeys(ctx context.Context, cacheClient cache.Client) ([]string, error) {
	var keysToDelete []string
	for _, pattern := range []string{
		sharedalarmkeys.ChannelSubscribersKeyPrefix + "*",
		sharedalarmkeys.ChannelSubscribersEmptyKeyPrefix + "*",
	} {
		keys, scanErr := cacheClient.ScanKeys(ctx, pattern, cacheScanBatchSize)
		if scanErr != nil {
			return nil, fmt.Errorf("rebuild subscriber cache from alarms: scan keys %q: %w", pattern, scanErr)
		}
		keysToDelete = append(keysToDelete, keys...)
	}
	return keysToDelete, nil
}

func deleteSubscriberCacheKeys(ctx context.Context, cacheClient cache.Client, keysToDelete []string) error {
	keysToDelete = compactUniqueStrings(keysToDelete)
	if len(keysToDelete) == 0 {
		return nil
	}

	if _, err := cacheClient.DelMany(ctx, keysToDelete); err != nil {
		return fmt.Errorf("rebuild subscriber cache from alarms: delete existing keys: %w", err)
	}

	return nil
}

func scanRoomAlarmKeys(ctx context.Context, cacheClient cache.Client) ([]string, error) {
	keys, err := cacheClient.ScanKeys(ctx, sharedalarmkeys.AlarmKeyPrefix+"*", cacheScanBatchSize)
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

func WarmSubscriberCacheFromAlarms(ctx context.Context, cacheClient cache.Client, alarms []*domain.Alarm) (CacheWarmSummary, error) {
	if cacheClient == nil {
		return CacheWarmSummary{}, errors.New("warm subscriber cache from alarms: cache service is nil")
	}

	warmData := newSubscriberCacheWarmData(alarms)
	for _, alarmRecord := range alarms {
		warmData.addAlarm(alarmRecord)
	}

	if err := writeSubscriberCacheWarmData(ctx, cacheClient, warmData); err != nil {
		return CacheWarmSummary{}, err
	}

	return warmData.finish(), nil
}

func normalizedWarmAlarmIdentity(alarmRecord *domain.Alarm) (normalizedRoomID, normalizedChannelID string, ok bool) {
	if alarmRecord == nil {
		return "", "", false
	}
	roomID := strings.TrimSpace(alarmRecord.RoomID)
	channelID := strings.TrimSpace(alarmRecord.ChannelID)
	return roomID, channelID, roomID != "" && channelID != ""
}

func writeSubscriberCacheWarmData(ctx context.Context, cacheClient cache.Client, data *subscriberCacheWarmData) error {
	if err := writeSubscriberCacheSets(ctx, cacheClient, data); err != nil {
		return err
	}
	if err := writeSubscriberCacheHashes(ctx, cacheClient, data); err != nil {
		return err
	}
	return writeSubscriberCacheWarmMarkers(ctx, cacheClient, data.summary.AlarmCount == 0)
}

func writeSubscriberCacheSets(ctx context.Context, cacheClient cache.Client, data *subscriberCacheWarmData) error {
	if err := writeWarmSetMap(ctx, cacheClient, data.roomAlarmMembers, "room alarms"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}
	if err := writeWarmSet(ctx, cacheClient, sharedalarmkeys.AlarmRegistryKey, compactUniqueStrings(data.registryRooms), "room registry"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}
	if err := writeWarmSet(ctx, cacheClient, sharedalarmkeys.AlarmChannelRegistryKey, compactUniqueStrings(data.channelRegistry), "channel registry"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}
	if err := writeWarmSetMap(ctx, cacheClient, data.channelSubscribers, "channel subscribers"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}
	return nil
}

func writeSubscriberCacheHashes(ctx context.Context, cacheClient cache.Client, data *subscriberCacheWarmData) error {
	if err := writeWarmHash(ctx, cacheClient, sharedalarmkeys.MemberNameKey, data.memberNames); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: cache member names: %w", err)
	}
	if err := writeWarmHash(ctx, cacheClient, sharedalarmkeys.RoomNamesCacheKey, data.roomNames); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: cache room names: %w", err)
	}
	if err := writeWarmHash(ctx, cacheClient, sharedalarmkeys.UserNamesCacheKey, data.userNames); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: cache user names: %w", err)
	}
	return nil
}

func writeSubscriberCacheWarmMarkers(ctx context.Context, cacheClient cache.Client, empty bool) error {
	if err := markSubscriberCacheEmptyState(ctx, cacheClient, empty); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: mark empty state: %w", err)
	}
	if err := bumpAlarmChannelRegistryVersion(ctx, cacheClient); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: bump channel registry version: %w", err)
	}
	return nil
}

func bumpAlarmChannelRegistryVersion(ctx context.Context, cacheClient cache.Client) error {
	return cacheClient.Set(ctx, sharedalarmkeys.AlarmChannelRegistryVersionKey, time.Now().UTC().UnixNano(), 0)
}

func markSubscriberCacheEmptyState(ctx context.Context, cacheClient cache.Client, empty bool) error {
	if empty {
		return cacheClient.Set(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey, "1", 0)
	}

	return cacheClient.Del(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
}
