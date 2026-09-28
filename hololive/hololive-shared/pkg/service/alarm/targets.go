package alarm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

var channelSubscriberLoadGroup singleflight.Group

func LookupChannelSubscribersByType(
	ctx context.Context,
	cacheClient cache.Client,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	if cacheClient == nil {
		return nil, errors.New("lookup channel subscribers by type: cache service is nil")
	}

	normalizedChannelID := strings.TrimSpace(channelID)
	if normalizedChannelID == "" {
		return nil, nil
	}

	key := sharedalarmkeys.BuildChannelSubscriberKey(normalizedChannelID, alarmType)

	subscribers, err := cacheClient.SMembers(ctx, key)
	if err != nil {
		return nil, fmt.Errorf(
			"lookup channel subscribers by type: channel %s type %s: %w",
			normalizedChannelID,
			alarmType,
			err,
		)
	}

	return subscribers, nil
}

func ResolveChannelSubscribersByType(
	ctx context.Context,
	cacheClient cache.Client,
	db dbx.Querier,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	normalizedChannelID := strings.TrimSpace(channelID)
	if normalizedChannelID == "" {
		return nil, nil
	}

	if cacheClient != nil {
		subscribers, resolved, err := resolveChannelSubscribersFromCache(ctx, cacheClient, normalizedChannelID, alarmType, db == nil)
		if err != nil {
			return subscribers, fmt.Errorf("resolve channel subscribers from cache: %w", err)
		}

		if resolved {
			return subscribers, nil
		}
	}

	out, err := resolveChannelSubscribersFromDB(ctx, cacheClient, db, normalizedChannelID, alarmType)
	if err != nil {
		return out, fmt.Errorf("resolve channel subscribers from DB: %w", err)
	}

	return out, nil
}

func resolveChannelSubscribersFromCache(
	ctx context.Context,
	cacheClient cache.Client,
	channelID string,
	alarmType domain.AlarmType,
	requireCacheSuccess bool,
) ([]string, bool, error) {
	subscribers, err := LookupChannelSubscribersByType(ctx, cacheClient, channelID, alarmType)
	if err != nil {
		observeAlarmSubscriberCacheError("lookup")

		if requireCacheSuccess {
			return nil, true, fmt.Errorf("resolve channel subscribers by type: %w", err)
		}

		return nil, false, nil
	}

	normalizedSubscribers := normalizeSubscriberIDs(subscribers)
	if len(normalizedSubscribers) > 0 {
		return normalizedSubscribers, true, nil
	}

	resolved, err := resolveKnownEmptySubscriberCache(ctx, cacheClient, channelID, alarmType, requireCacheSuccess)
	if err != nil {
		return nil, resolved, fmt.Errorf("resolve known empty subscriber cache: %w", err)
	}

	return nil, resolved, nil
}

func resolveKnownEmptySubscriberCache(
	ctx context.Context,
	cacheClient cache.Client,
	channelID string,
	alarmType domain.AlarmType,
	requireCacheSuccess bool,
) (bool, error) {
	isKnownEmpty, err := cacheClient.Exists(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType))
	if err == nil {
		return isKnownEmpty, nil
	}

	observeAlarmSubscriberCacheError("check_empty")

	if requireCacheSuccess {
		return true, fmt.Errorf("resolve channel subscribers by type: check empty subscriber cache: %w", err)
	}

	return false, nil
}

func resolveChannelSubscribersFromDB(
	ctx context.Context,
	cacheClient cache.Client,
	db dbx.Querier,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	alarms, err := loadChannelSubscriberAlarms(ctx, db, channelID, alarmType)
	if err != nil {
		observeAlarmSubscriberDBFallback("error")

		return nil, fmt.Errorf("resolve channel subscribers by type: %w", err)
	}

	subscribers := extractSubscriberIDsByType(alarms, alarmType)
	if len(subscribers) == 0 {
		observeAlarmSubscriberDBFallback("miss")
		markEmptyChannelSubscriberCache(ctx, cacheClient, channelID, alarmType)

		return nil, nil
	}

	observeAlarmSubscriberDBFallback("hit")

	warmChannelSubscriberCache(ctx, cacheClient, alarms, channelID, alarmType)

	return subscribers, nil
}

// ResolveUncachedChannelSubscribersByType는 구독 cache set이 비어 있던 채널들의 구독자를 확정한다.
// TTL 없는 set이 eviction으로 사라지면 빈 set과 구분되지 않으므로, empty marker가 있는 채널만 진짜 구독 0으로 보고
// 나머지는 한 번의 DB 조회로 확정한다. 구독자가 있는 채널만 결과 map에 담는다.
//
// Checker 경로의 read-through라 PG 결과는 이번 호출에만 쓰고 subscriber set을 다시 채우지 않는다. 조회 뒤 SADD하면
// 그 사이 커밋된 구독 해지의 SREM보다 늦게 도착해 해지된 방이 set에 되살아날 수 있기 때문이다. 대가로 다른 쓰기 경로
// (구독 변경 동기화·전체 rebuild)가 set을 다시 채울 때까지 evict된 채널은 매 cycle 이 batch PG 조회 1회를 다시 치른다.
// 구독 0 채널의 empty marker 기록은 유지한다.
func ResolveUncachedChannelSubscribersByType(
	ctx context.Context,
	cacheClient cache.Client,
	db dbx.Querier,
	channelIDs []string,
	alarmType domain.AlarmType,
) (map[string][]string, error) {
	pending, err := filterKnownEmptySubscriberChannels(ctx, cacheClient, channelIDs, alarmType, db == nil)
	if err != nil {
		return nil, fmt.Errorf("resolve uncached channel subscribers: %w", err)
	}

	result := make(map[string][]string, len(pending))
	if len(pending) == 0 {
		return result, nil
	}

	alarmsByChannel, err := loadChannelSubscriberAlarmsByChannels(ctx, db, pending, alarmType)
	if err != nil {
		observeAlarmSubscriberDBFallback("error")

		return nil, fmt.Errorf("resolve uncached channel subscribers: %w", err)
	}

	for _, channelID := range pending {
		alarms := alarmsByChannel[channelID]

		subscribers := extractSubscriberIDsByType(alarms, alarmType)
		if len(subscribers) == 0 {
			observeAlarmSubscriberDBFallback("miss")
			markEmptyChannelSubscriberCache(ctx, cacheClient, channelID, alarmType)

			continue
		}

		observeAlarmSubscriberDBFallback("hit")

		result[channelID] = subscribers
	}

	return result, nil
}

// filterKnownEmptySubscriberChannels는 empty marker로 구독 0이 확정된 채널을 빼고 DB 확인이 필요한 채널만 남긴다.
func filterKnownEmptySubscriberChannels(
	ctx context.Context,
	cacheClient cache.Client,
	channelIDs []string,
	alarmType domain.AlarmType,
	requireCacheSuccess bool,
) ([]string, error) {
	pending := make([]string, 0, len(channelIDs))
	seen := make(map[string]struct{}, len(channelIDs))

	for _, channelID := range channelIDs {
		normalizedChannelID := strings.TrimSpace(channelID)
		if normalizedChannelID == "" {
			continue
		}

		if _, exists := seen[normalizedChannelID]; exists {
			continue
		}

		seen[normalizedChannelID] = struct{}{}

		if cacheClient != nil {
			knownEmpty, err := resolveKnownEmptySubscriberCache(ctx, cacheClient, normalizedChannelID, alarmType, requireCacheSuccess)
			if err != nil {
				return nil, fmt.Errorf("filter known empty subscriber channels: %w", err)
			}

			if knownEmpty {
				continue
			}
		}

		pending = append(pending, normalizedChannelID)
	}

	return pending, nil
}

func loadChannelSubscriberAlarmsByChannels(
	ctx context.Context,
	db dbx.Querier,
	channelIDs []string,
	alarmType domain.AlarmType,
) (map[string][]*domain.Alarm, error) {
	if db == nil {
		return nil, errors.New("load channel subscriber alarms by channels: database is nil")
	}

	if !domain.AlarmTypes(domain.AllAlarmTypes).Contains(alarmType) {
		return map[string][]*domain.Alarm{}, nil
	}

	out, err := newRepositoryWithQuerier(db).loadChannelSubscriberAlarmsByChannels(ctx, channelIDs, alarmType)
	if err != nil {
		return nil, fmt.Errorf("load channel subscriber alarms by channels: %w", err)
	}

	return out, nil
}

func loadChannelSubscriberAlarms(ctx context.Context, db dbx.Querier, channelID string, alarmType domain.AlarmType) ([]*domain.Alarm, error) {
	if db == nil {
		return nil, errors.New("load channel subscriber alarms: database is nil")
	}

	if !domain.AlarmTypes(domain.AllAlarmTypes).Contains(alarmType) {
		return nil, nil
	}

	normalizedChannelID := strings.TrimSpace(channelID)
	loadKey := normalizedChannelID + "\x00" + string(alarmType)
	repository := newRepositoryWithQuerier(db)
	resultCh := channelSubscriberLoadGroup.DoChan(loadKey, func() (any, error) {
		return repository.loadChannelSubscriberAlarms(ctx, normalizedChannelID, alarmType)
	})

	out, err := waitForChannelSubscriberAlarms(ctx, resultCh)
	if err != nil {
		return out, fmt.Errorf("wait for channel subscriber alarms: %w", err)
	}

	return out, nil
}

func waitForChannelSubscriberAlarms(ctx context.Context, resultCh <-chan singleflight.Result) ([]*domain.Alarm, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("load channel subscriber alarms: wait for shared query: %w", ctx.Err())
	case result := <-resultCh:
		out, err := resolveChannelSubscriberLoadResult(result)
		if err != nil {
			return out, fmt.Errorf("resolve channel subscriber load result: %w", err)
		}

		return out, nil
	}
}

func resolveChannelSubscriberLoadResult(result singleflight.Result) ([]*domain.Alarm, error) {
	if result.Err != nil {
		return nil, result.Err
	}

	if result.Shared {
		observeAlarmSubscriberDBSingleflightShared()
	}

	sharedAlarms, ok := result.Val.([]*domain.Alarm)
	if !ok {
		return nil, fmt.Errorf("load channel subscriber alarms: unexpected singleflight result type %T", result.Val)
	}

	return cloneAlarmRecords(sharedAlarms), nil
}

func cloneAlarmRecords(alarms []*domain.Alarm) []*domain.Alarm {
	if len(alarms) == 0 {
		return nil
	}

	cloned := make([]*domain.Alarm, 0, len(alarms))
	for _, alarmRecord := range alarms {
		if alarmRecord == nil {
			cloned = append(cloned, nil)
			continue
		}

		recordCopy := *alarmRecord
		if len(alarmRecord.AlarmTypes) > 0 {
			recordCopy.AlarmTypes = append(domain.AlarmTypes(nil), alarmRecord.AlarmTypes...)
		}

		cloned = append(cloned, &recordCopy)
	}

	return cloned
}

func extractSubscriberIDsByType(alarms []*domain.Alarm, alarmType domain.AlarmType) []string {
	subscribers := make([]string, 0, len(alarms))
	seen := make(map[string]struct{}, len(alarms))

	for _, alarmRecord := range alarms {
		subscribers = appendSubscriberIDByType(subscribers, seen, alarmRecord, alarmType)
	}

	return subscribers
}

func appendSubscriberIDByType(
	subscribers []string,
	seen map[string]struct{},
	alarmRecord *domain.Alarm,
	alarmType domain.AlarmType,
) []string {
	if !alarmRecordMatchesType(alarmRecord, alarmType) {
		return subscribers
	}

	subscriberID := strings.TrimSpace(alarmRecord.RegistryKey())
	if subscriberID == "" {
		return subscribers
	}

	if _, exists := seen[subscriberID]; exists {
		return subscribers
	}

	seen[subscriberID] = struct{}{}

	return append(subscribers, subscriberID)
}

func alarmRecordMatchesType(alarmRecord *domain.Alarm, alarmType domain.AlarmType) bool {
	if alarmRecord == nil {
		return false
	}

	alarmTypes := alarmRecord.AlarmTypes
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}

	return alarmTypes.Contains(alarmType)
}

func normalizeSubscriberIDs(subscribers []string) []string {
	normalized := make([]string, 0, len(subscribers))
	seen := make(map[string]struct{}, len(subscribers))

	for _, subscriberID := range subscribers {
		trimmedSubscriberID := strings.TrimSpace(subscriberID)
		if trimmedSubscriberID == "" {
			continue
		}

		if _, exists := seen[trimmedSubscriberID]; exists {
			continue
		}

		seen[trimmedSubscriberID] = struct{}{}
		normalized = append(normalized, trimmedSubscriberID)
	}

	return normalized
}
