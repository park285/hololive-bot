package alarmstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type CacheWarmSummary struct {
	AlarmCount   int
	RoomCount    int
	ChannelCount int
}

type subscriberCacheWarmSource interface {
	LoadAll(ctx context.Context) ([]*domain.Alarm, error)
}

const cacheScanBatchSize int64 = 100

var subscriberCacheStaticKeys = []string{
	alarmkeys.AlarmChannelRegistryKey,
	alarmkeys.AlarmSubscriberCacheEmptyKey,
}

var subscriberCacheKeyPatterns = []string{
	alarmkeys.ChannelSubscribersKeyPrefix + "*",
}

type SubscriberCacheStore interface {
	cache.KeyValueCache
	cache.LowLevelCache
}

func RebuildSubscriberCacheFromRepository(ctx context.Context, cacheClient SubscriberCacheStore, repository *Repository) (CacheWarmSummary, error) {
	if repository == nil {
		return CacheWarmSummary{}, errors.New("rebuild subscriber cache from repository: repository is nil")
	}

	return rebuildSubscriberCache(ctx, cacheClient, repository)
}

func rebuildSubscriberCache(ctx context.Context, cacheClient SubscriberCacheStore, source subscriberCacheWarmSource) (CacheWarmSummary, error) {
	if err := clearSubscriberCacheNamespace(ctx, cacheClient); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("clear subscriber cache namespace: %w", err)
	}

	warmData, err := loadSubscriberCacheWarmData(ctx, source)
	if err != nil {
		return CacheWarmSummary{}, fmt.Errorf("load subscriber cache warm data: %w", err)
	}

	if err := writeSubscriberCacheWarmData(ctx, cacheClient, warmData); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("write subscriber cache warm data: %w", err)
	}

	return warmData.summary, nil
}

func loadSubscriberCacheWarmData(ctx context.Context, source subscriberCacheWarmSource) (*subscriberCacheWarmData, error) {
	alarms, err := source.LoadAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebuild subscriber cache from repository: load alarms: %w", err)
	}

	warmData := newSubscriberCacheWarmData(alarms)
	for _, alarmRecord := range alarms {
		warmData.addAlarm(alarmRecord)
	}

	warmData.summary = warmData.finish()

	return warmData, nil
}

func clearSubscriberCacheNamespace(ctx context.Context, cacheClient cache.KeyValueCache) error {
	if cacheClient == nil {
		return errors.New("rebuild subscriber cache from alarms: cache service is nil")
	}

	for _, pattern := range subscriberCacheKeyPatterns {
		err := cacheClient.ScanKeyPages(ctx, pattern, cacheScanBatchSize, func(keys []string) error {
			return deleteSubscriberCacheKeys(ctx, cacheClient, keys)
		})
		if err != nil {
			return fmt.Errorf("rebuild subscriber cache from alarms: clear keys %q: %w", pattern, err)
		}
	}

	if err := deleteSubscriberCacheKeys(ctx, cacheClient, subscriberCacheStaticKeys); err != nil {
		return fmt.Errorf("delete subscriber cache static keys: %w", err)
	}

	return nil
}

func deleteSubscriberCacheKeys(ctx context.Context, cacheClient cache.KeyValueLifecycle, keysToDelete []string) error {
	keysToDelete = compactUniqueStrings(keysToDelete)
	if len(keysToDelete) == 0 {
		return nil
	}

	if _, err := cacheClient.DelMany(ctx, keysToDelete); err != nil {
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

func normalizedWarmAlarmIdentity(alarmRecord *domain.Alarm) (normalizedRoomID, normalizedChannelID string, ok bool) {
	if alarmRecord == nil {
		return "", "", false
	}

	roomID := strings.TrimSpace(alarmRecord.RoomID)
	channelID := strings.TrimSpace(alarmRecord.ChannelID)

	return roomID, channelID, roomID != "" && channelID != ""
}

func writeSubscriberCacheWarmData(ctx context.Context, cacheClient SubscriberCacheStore, data *subscriberCacheWarmData) error {
	if err := writeSubscriberCacheSets(ctx, cacheClient, data); err != nil {
		return fmt.Errorf("write subscriber cache sets: %w", err)
	}

	if err := markSubscriberCacheEmptyState(ctx, cacheClient, data.summary.AlarmCount == 0); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: mark empty state: %w", err)
	}

	return nil
}

func writeSubscriberCacheSets(ctx context.Context, cacheClient cache.LowLevelCache, data *subscriberCacheWarmData) error {
	if err := writeWarmSet(ctx, cacheClient, alarmkeys.AlarmChannelRegistryKey, compactUniqueStrings(data.channelRegistry), "channel registry"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}

	if err := writeWarmSetMap(ctx, cacheClient, data.channelSubscribers, "channel subscribers"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}

	return nil
}

func markSubscriberCacheEmptyState(ctx context.Context, cacheClient cache.KeyValueCache, empty bool) error {
	if empty {
		if err := cacheClient.Set(ctx, alarmkeys.AlarmSubscriberCacheEmptyKey, "1", 0); err != nil {
			return fmt.Errorf("set: %w", err)
		}

		return nil
	}

	if err := cacheClient.Del(ctx, alarmkeys.AlarmSubscriberCacheEmptyKey); err != nil {
		return fmt.Errorf("del: %w", err)
	}

	return nil
}
