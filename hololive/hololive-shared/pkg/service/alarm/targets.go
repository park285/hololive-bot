package alarm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valkey-io/valkey-go"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// SubscriberResolver는 하나의 구독 cache와 구독 DB에 묶인 구독자 조회기다.
// 같은 resolver로 들어온 같은 채널·알림 종류의 DB 조회만 하나로 합친다. 다른 DB, 다른 트랜잭션에 묶인 resolver와는
// 조회 결과나 오류를 공유하지 않으므로 pool 조회는 연결 수명 동안 resolver 하나를 재사용하고, 트랜잭션 안의 조회는
// 그 트랜잭션 전용 resolver를 만든다. CacheClient가 nil이면 cache를 건너뛰고, db가 nil이면 cache로 확정되지 않는
// 조회를 오류로 반환한다. 복사하지 말고 반환된 포인터를 공유한다.
type SubscriberResolver struct {
	cache cache.Client
	db    dbx.Querier
	loads singleflight.Group
}

// NewSubscriberResolver는 cacheClient와 db에 묶인 구독자 조회기를 만든다. 두 인자 모두 nil을 허용한다.
func NewSubscriberResolver(cacheClient cache.Client, db dbx.Querier) *SubscriberResolver {
	return &SubscriberResolver{cache: cacheClient, db: db}
}

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

// ResolveChannelSubscribersByType는 채널·알림 종류의 구독 방을 cache에서 찾고, cache로 확정되지 않으면 resolver의
// DB에서 읽는다. DB 결과는 이번 호출에만 쓰고 cache를 다시 채우지 않는다.
func (r *SubscriberResolver) ResolveChannelSubscribersByType(
	ctx context.Context,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	normalizedChannelID := strings.TrimSpace(channelID)
	if normalizedChannelID == "" {
		return nil, nil
	}

	if r.cache != nil {
		subscribers, resolved, err := resolveChannelSubscribersFromCache(ctx, r.cache, normalizedChannelID, alarmType, r.db == nil)
		if err != nil {
			return subscribers, fmt.Errorf("resolve channel subscribers from cache: %w", err)
		}

		if resolved {
			return subscribers, nil
		}
	}

	out, err := r.resolveChannelSubscribersFromDB(ctx, normalizedChannelID, alarmType)
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

func (r *SubscriberResolver) resolveChannelSubscribersFromDB(
	ctx context.Context,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	alarms, err := r.loadChannelSubscriberAlarms(ctx, channelID, alarmType)
	if err != nil {
		observeAlarmSubscriberDBFallback(subscriberDBFallbackError)

		return nil, fmt.Errorf("resolve channel subscribers by type: %w", err)
	}

	subscribers := extractSubscriberIDsByType(alarms, alarmType)
	if len(subscribers) == 0 {
		observeAlarmSubscriberDBFallback(subscriberDBFallbackMiss)

		return nil, nil
	}

	observeAlarmSubscriberDBFallback(subscriberDBFallbackHit)

	return subscribers, nil
}

// ResolveUncachedChannelSubscribersByType는 구독 cache set이 비어 있던 채널들의 구독자를 확정한다.
// TTL 없는 set이 eviction으로 사라지면 빈 set과 구분되지 않으므로, empty marker가 있는 채널만 진짜 구독 0으로 보고
// 나머지는 한 번의 DB 조회로 확정한다. 구독자가 있는 채널만 결과 map에 담는다.
//
// Checker 경로의 read-through라 PG 결과는 이번 호출에만 쓰고 subscriber set을 다시 채우지 않는다. 조회 뒤 SADD하면
// 그 사이 커밋된 구독 해지의 SREM보다 늦게 도착해 해지된 방이 set에 되살아날 수 있기 때문이다. 대가로 다른 쓰기 경로
// (구독 변경 동기화·전체 rebuild)가 set을 다시 채울 때까지 evict된 채널은 매 cycle 이 batch PG 조회 1회를 다시 치른다.
// 빈 구독 marker도 늦은 조회 결과가 새 구독을 숨길 수 있으므로 read-through에서 기록하지 않는다.
func (r *SubscriberResolver) ResolveUncachedChannelSubscribersByType(
	ctx context.Context,
	channelIDs []string,
	alarmType domain.AlarmType,
) (map[string][]string, error) {
	pending, err := filterKnownEmptySubscriberChannels(ctx, r.cache, channelIDs, alarmType, r.db == nil)
	if err != nil {
		return nil, fmt.Errorf("resolve uncached channel subscribers: %w", err)
	}

	result := make(map[string][]string, len(pending))
	if len(pending) == 0 {
		return result, nil
	}

	alarmsByChannel, err := loadChannelSubscriberAlarmsByChannels(ctx, r.db, pending, alarmType)
	if err != nil {
		observeAlarmSubscriberDBFallback(subscriberDBFallbackError)

		return nil, fmt.Errorf("resolve uncached channel subscribers: %w", err)
	}

	for _, channelID := range pending {
		alarms := alarmsByChannel[channelID]

		subscribers := extractSubscriberIDsByType(alarms, alarmType)
		if len(subscribers) == 0 {
			observeAlarmSubscriberDBFallback(subscriberDBFallbackMiss)

			continue
		}

		observeAlarmSubscriberDBFallback(subscriberDBFallbackHit)

		result[channelID] = subscribers
	}

	return result, nil
}

// filterKnownEmptySubscriberChannels는 empty marker로 구독 0이 확정된 채널을 빼고 DB 확인이 필요한 채널만 남긴다.
// 정규화·중복 제거한 채널의 marker 확인은 한 번의 pipeline으로 보낸다.
func filterKnownEmptySubscriberChannels(
	ctx context.Context,
	cacheClient cache.Client,
	channelIDs []string,
	alarmType domain.AlarmType,
	requireCacheSuccess bool,
) ([]string, error) {
	unique := make([]string, 0, len(channelIDs))
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
		unique = append(unique, normalizedChannelID)
	}

	if cacheClient == nil || len(unique) == 0 {
		return unique, nil
	}

	knownEmpty, err := resolveKnownEmptySubscriberChannels(ctx, cacheClient, unique, alarmType, requireCacheSuccess)
	if err != nil {
		return nil, fmt.Errorf("filter known empty subscriber channels: %w", err)
	}

	pending := unique[:0]

	for i, channelID := range unique {
		if !knownEmpty[i] {
			pending = append(pending, channelID)
		}
	}

	return pending, nil
}

// resolveKnownEmptySubscriberChannels는 채널마다 empty marker EXISTS를 한 번의 DoMulti pipeline으로 보내고 입력과
// 같은 위치에 marker 존재 여부를 돌려준다. 확인에 실패한 채널은 구독 0으로 단정하지 않는다. DB가 없어 cache 결과만
// 믿어야 하면(requireCacheSuccess) 실패를 오류로 반환한다.
func resolveKnownEmptySubscriberChannels(
	ctx context.Context,
	cacheClient cache.Client,
	channelIDs []string,
	alarmType domain.AlarmType,
	requireCacheSuccess bool,
) ([]bool, error) {
	builder := cacheClient.Builder()
	cmds := make([]valkey.Completed, len(channelIDs))

	for i, channelID := range channelIDs {
		cmds[i] = builder.Exists().Key(sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType)).Build()
	}

	knownEmpty := make([]bool, len(channelIDs))

	results := cacheClient.DoMulti(ctx, cmds...)
	if len(results) != len(cmds) {
		observeAlarmSubscriberCacheError("check_empty")

		if requireCacheSuccess {
			return nil, fmt.Errorf(
				"resolve channel subscribers by type: check empty subscriber cache: unexpected result count %d for %d channels",
				len(results),
				len(cmds),
			)
		}

		return knownEmpty, nil
	}

	for i, result := range results {
		count, err := result.AsInt64()
		if err != nil {
			observeAlarmSubscriberCacheError("check_empty")

			if requireCacheSuccess {
				return nil, fmt.Errorf("resolve channel subscribers by type: check empty subscriber cache: %w", err)
			}

			continue
		}

		knownEmpty[i] = count > 0
	}

	return knownEmpty, nil
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

func (r *SubscriberResolver) loadChannelSubscriberAlarms(
	ctx context.Context,
	channelID string,
	alarmType domain.AlarmType,
) ([]*domain.Alarm, error) {
	if r.db == nil {
		return nil, errors.New("load channel subscriber alarms: database is nil")
	}

	if !domain.AlarmTypes(domain.AllAlarmTypes).Contains(alarmType) {
		return nil, nil
	}

	normalizedChannelID := strings.TrimSpace(channelID)
	loadKey := normalizedChannelID + "\x00" + string(alarmType)
	repository := newRepositoryWithQuerier(r.db)
	resultCh := r.loads.DoChan(loadKey, func() (any, error) {
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
