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

// RebuildSubscriberCacheFromRepository는 전체 subscriber cache를 정본 구독으로 다시 채운다.
// 호출자는 모든 구독 변경과 같은 mutation mutex를 보유해야 한다. 현재 단일 alarm-worker의 AlarmService가
// 이 직렬화를 소유하며, 여러 프로세스에서 같은 cache를 쓰려면 rebuild와 변경을 함께 fence해야 한다.
func RebuildSubscriberCacheFromRepository(ctx context.Context, cacheClient cache.Client, repository *Repository) (CacheWarmSummary, error) {
	if repository == nil {
		return CacheWarmSummary{}, errors.New("rebuild subscriber cache from repository: repository is nil")
	}

	// 모든 페이지 삭제를 끝낸 뒤 DB 스냅샷을 읽는다. 일부 삭제가 실패한 경우 오래된 snapshot을 다시 쓰지 않고
	// 오류를 반환하며, 사라진 set은 구독 조회의 기존 DB read-through로 확정한다.
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

// clearSubscriberCacheNamespace는 subscriber cache key를 SCAN page마다 바로 지워 전체 key 목록을 메모리에 모으지 않는다.
// 모든 page 삭제가 끝난 뒤에만 반환하므로 호출자의 clear→load 순서가 유지되고, 삭제가 SCAN 순회와 겹쳐도 SCAN은
// 순회 내내 존재한 key를 빠뜨리지 않는다. 순회 중 새로 생긴 key는 이후 load한 DB 스냅샷에도 반영돼 있어 지워져도 복원된다.
func clearSubscriberCacheNamespace(ctx context.Context, cacheClient cache.Client) error {
	if cacheClient == nil {
		return errors.New("rebuild subscriber cache from alarms: cache service is nil")
	}

	for _, pattern := range []string{
		sharedalarmkeys.ChannelSubscribersKeyPrefix + "*",
		sharedalarmkeys.ChannelSubscribersEmptyKeyPrefix + "*",
	} {
		err := cacheClient.ScanKeyPages(ctx, pattern, cacheScanBatchSize, func(keys []string) error {
			return deleteSubscriberCacheKeys(ctx, cacheClient, keys)
		})
		if err != nil {
			return fmt.Errorf("rebuild subscriber cache from alarms: clear keys %q: %w", pattern, err)
		}
	}

	// 페이지 순회가 실패하면 이미 존재하는 전체 채널 registry는 보존한다. 구독 set이 일부 지워져도
	// 대상 채널은 유지되며, 누락된 set은 기존 DB read-through가 확정한다.
	if err := deleteSubscriberCacheKeys(ctx, cacheClient, subscriberCacheStaticKeys); err != nil {
		return fmt.Errorf("delete subscriber cache static keys: %w", err)
	}

	return nil
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
