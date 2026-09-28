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

var loadAllAlarmsFromRepository = func(ctx context.Context, repository *Repository) ([]*domain.Alarm, error) {
	return repository.LoadAll(ctx)
}

var loadMemberNamesFromRepository = func(ctx context.Context, repository *Repository) (map[string]string, error) {
	return repository.GetAllMemberNames(ctx)
}

const cacheScanBatchSize int64 = 100

// subscriberCacheStaticKeys는 rebuild가 지우고 다시 채우는 고정 이름 key다. 방 목록·방 이름·사용자 이름은 PG가
// 원천이라 subscriber cache에 두지 않는다.
var subscriberCacheStaticKeys = []string{
	sharedalarmkeys.AlarmChannelRegistryKey,
	sharedalarmkeys.AlarmSubscriberCacheEmptyKey,
	sharedalarmkeys.MemberNameKey,
}

func RebuildSubscriberCacheFromRepository(ctx context.Context, cacheClient cache.Client, repository *Repository) (CacheWarmSummary, error) {
	if repository == nil {
		return CacheWarmSummary{}, errors.New("rebuild subscriber cache from repository: repository is nil")
	}

	// clear는 DB 스냅샷보다 먼저 실행해야 한다. 스냅샷→clear 순서에서는 스냅샷 채취 후
	// 커밋된 구독의 SAdd가 clear에 지워지고 스냅샷에도 없어 다음 rebuild까지 영구
	// 소실된다. clear→load 순서면 clear 이후의 SAdd는 warm SAdd와 병합되고, clear
	// 이전의 add는 스냅샷에 포함되어 양쪽 경쟁 창이 모두 닫힌다.
	if err := clearSubscriberCacheNamespace(ctx, cacheClient); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("clear subscriber cache namespace: %w", err)
	}

	warmData, err := loadSubscriberCacheWarmData(ctx, repository)
	if err != nil {
		return CacheWarmSummary{}, fmt.Errorf("load subscriber cache warm data: %w", err)
	}

	if err := writeSubscriberCacheWarmData(ctx, cacheClient, warmData); err != nil {
		return CacheWarmSummary{}, fmt.Errorf("write subscriber cache warm data: %w", err)
	}

	return warmData.summary, nil
}

func loadSubscriberCacheWarmData(ctx context.Context, repository *Repository) (*subscriberCacheWarmData, error) {
	alarms, err := loadAllAlarmsFromRepository(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("rebuild subscriber cache from repository: load alarms: %w", err)
	}

	warmData := newSubscriberCacheWarmData(alarms)
	for _, alarmRecord := range alarms {
		warmData.addAlarm(alarmRecord)
	}

	warmData.summary = warmData.finish()

	memberNames, err := loadMemberNamesFromRepository(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("rebuild subscriber cache from repository: load member names: %w", err)
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

	keysToDelete := append([]string(nil), subscriberCacheStaticKeys...)

	patternKeys, err := scanSubscriberCachePatternKeys(ctx, cacheClient)
	if err != nil {
		return fmt.Errorf("scan subscriber cache pattern keys: %w", err)
	}

	keysToDelete = append(keysToDelete, patternKeys...)

	if err := deleteSubscriberCacheKeys(ctx, cacheClient, keysToDelete); err != nil {
		return fmt.Errorf("delete subscriber cache keys: %w", err)
	}

	return nil
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

func writeSubscriberCacheWarmData(ctx context.Context, cacheClient cache.Client, data *subscriberCacheWarmData) error {
	if err := writeSubscriberCacheSets(ctx, cacheClient, data); err != nil {
		return fmt.Errorf("write subscriber cache sets: %w", err)
	}

	if err := writeWarmHash(ctx, cacheClient, sharedalarmkeys.MemberNameKey, data.memberNames); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: cache member names: %w", err)
	}

	if err := markSubscriberCacheEmptyState(ctx, cacheClient, data.summary.AlarmCount == 0); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: mark empty state: %w", err)
	}

	return nil
}

func writeSubscriberCacheSets(ctx context.Context, cacheClient cache.Client, data *subscriberCacheWarmData) error {
	if err := writeWarmSet(ctx, cacheClient, sharedalarmkeys.AlarmChannelRegistryKey, compactUniqueStrings(data.channelRegistry), "channel registry"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}

	if err := writeWarmSetMap(ctx, cacheClient, data.channelSubscribers, "channel subscribers"); err != nil {
		return fmt.Errorf("warm subscriber cache from alarms: %w", err)
	}

	return nil
}

func markSubscriberCacheEmptyState(ctx context.Context, cacheClient cache.Client, empty bool) error {
	if empty {
		if err := cacheClient.Set(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey, "1", 0); err != nil {
			return fmt.Errorf("set: %w", err)
		}

		return nil
	}

	if err := cacheClient.Del(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey); err != nil {
		return fmt.Errorf("del: %w", err)
	}

	return nil
}
