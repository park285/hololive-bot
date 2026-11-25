package alarmstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/park285/shared-go/postgres/pgxdb"
	"golang.org/x/sync/singleflight"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type SubscriberCache interface {
	cache.SetCache
	cache.LowLevelCache
}

type SubscriberResolver struct {
	cache SubscriberCache
	db    pgxdb.Querier
	loads singleflight.Group
}

func NewSubscriberResolver(cacheClient SubscriberCache, db pgxdb.Querier) *SubscriberResolver {
	return &SubscriberResolver{cache: cacheClient, db: db}
}

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

	out, err := r.resolveChannelSubscribersFromDB(ctx, normalizedChannelID, alarmType, r.cache != nil)
	if err != nil {
		return out, fmt.Errorf("resolve channel subscribers from DB: %w", err)
	}

	return out, nil
}

func resolveChannelSubscribersFromCache(
	ctx context.Context,
	cacheClient SubscriberCache,
	channelID string,
	alarmType domain.AlarmType,
	requireCacheSuccess bool,
) ([]string, bool, error) {
	subscribers, err := cacheClient.SMembers(ctx, alarmkeys.BuildChannelSubscriberKey(channelID, alarmType))
	if err != nil {
		observeAlarmSubscriberCacheError("lookup")

		if requireCacheSuccess {
			return nil, true, fmt.Errorf("resolve channel subscribers by type: lookup channel %s type %s: %w", channelID, alarmType, err)
		}

		return nil, false, nil
	}

	normalizedSubscribers := normalizeSubscriberIDs(subscribers)

	return normalizedSubscribers, len(normalizedSubscribers) > 0, nil
}

func (r *SubscriberResolver) resolveChannelSubscribersFromDB(
	ctx context.Context,
	channelID string,
	alarmType domain.AlarmType,
	countFallback bool,
) ([]string, error) {
	observe := func(result string) {
		if countFallback {
			observeAlarmSubscriberDBFallback(result)
		}
	}

	alarms, err := r.loadChannelSubscriberAlarms(ctx, channelID, alarmType)
	if err != nil {
		observe(subscriberDBFallbackError)

		return nil, fmt.Errorf("resolve channel subscribers by type: %w", err)
	}

	subscribers := extractSubscriberIDsByType(alarms, alarmType)
	if len(subscribers) == 0 {
		observe(subscriberDBFallbackMiss)

		return nil, nil
	}

	observe(subscriberDBFallbackHit)

	return subscribers, nil
}

func (r *SubscriberResolver) ResolveUncachedChannelSubscribersByType(
	ctx context.Context,
	channelIDs []string,
	alarmType domain.AlarmType,
) (map[string][]string, error) {
	pending := compactUniqueStrings(channelIDs)

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

func loadChannelSubscriberAlarmsByChannels(
	ctx context.Context,
	db pgxdb.Querier,
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

	return alarmRecord.AlarmTypes.Contains(alarmType)
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
