package alarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const emptyChannelSubscriberCacheTTL = 30 * time.Second
const channelSubscriberLoadTimeout = 5 * time.Second

var channelSubscriberLoadGroup singleflight.Group

func withoutCancelPreserveDeadline(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), fallback)
	}

	base := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(base, deadline)
	}

	return context.WithTimeout(base, fallback)
}

func LookupChannelSubscribersByType(
	ctx context.Context,
	cacheClient cache.Client,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	if cacheClient == nil {
		return nil, fmt.Errorf("lookup channel subscribers by type: cache service is nil")
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
		if resolved || err != nil {
			return subscribers, err
		}
	}

	return resolveChannelSubscribersFromDB(ctx, cacheClient, db, normalizedChannelID, alarmType)
}

func resolveChannelSubscribersFromCache(
	ctx context.Context,
	cacheClient cache.Client,
	channelID string,
	alarmType domain.AlarmType,
	requireCacheSuccess bool,
) (result0 []string, ok1 bool, err error) {
	subscribers, err := LookupChannelSubscribersByType(ctx, cacheClient, channelID, alarmType)
	if err != nil {
		if requireCacheSuccess {
			return nil, true, fmt.Errorf("resolve channel subscribers by type: %w", err)
		}
		return nil, false, nil
	}

	normalizedSubscribers := normalizeSubscriberIDs(subscribers)
	if len(normalizedSubscribers) > 0 {
		return normalizedSubscribers, true, nil
	}

	isKnownEmpty, err := cacheClient.Exists(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType))
	if err == nil && isKnownEmpty {
		return nil, true, nil
	}
	if err != nil && requireCacheSuccess {
		return nil, true, fmt.Errorf("resolve channel subscribers by type: check empty subscriber cache: %w", err)
	}
	return nil, false, nil
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

func markEmptyChannelSubscriberCache(ctx context.Context, cacheClient cache.Client, channelID string, alarmType domain.AlarmType) {
	if cacheClient == nil {
		return
	}
	if err := cacheClient.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType), "1", emptyChannelSubscriberCacheTTL); err != nil {
		observeAlarmSubscriberCacheError("mark_empty")
	}
}

func warmChannelSubscriberCache(ctx context.Context, cacheClient cache.Client, alarms []*domain.Alarm, channelID string, alarmType domain.AlarmType) {
	if cacheClient == nil {
		return
	}

	subscribers := extractSubscriberIDsByType(alarms, alarmType)
	key := sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)
	if err := writeWarmSet(ctx, cacheClient, key, subscribers, "channel subscribers"); err != nil {
		observeAlarmSubscriberCacheError("warm")
	}
	if err := cacheClient.Del(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType)); err != nil {
		observeAlarmSubscriberCacheError("clear_empty")
	}
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
	resultCh := channelSubscriberLoadGroup.DoChan(loadKey, func() (any, error) {
		return queryChannelSubscriberAlarms(ctx, db, normalizedChannelID, alarmType)
	})

	return waitForChannelSubscriberAlarms(ctx, resultCh)
}

func queryChannelSubscriberAlarms(ctx context.Context, db dbx.Querier, channelID string, alarmType domain.AlarmType) ([]*domain.Alarm, error) {
	queryCtx, cancel := withoutCancelPreserveDeadline(ctx, channelSubscriberLoadTimeout)
	defer cancel()

	rows, err := db.Query(queryCtx, mustSQL("targets_0188_01.sql"), channelID, string(alarmType))
	if err != nil {
		return nil, fmt.Errorf("load channel subscriber alarms: %w", err)
	}
	defer rows.Close()

	alarms := make([]*domain.Alarm, 0)
	for rows.Next() {
		alarmRecord, err := scanAlarmRow(rows)
		if err != nil {
			return nil, fmt.Errorf("load channel subscriber alarms: %w", err)
		}
		alarms = append(alarms, alarmRecord)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("load channel subscriber alarms: %w", rowsErr)
	}

	return alarms, nil
}

func waitForChannelSubscriberAlarms(ctx context.Context, resultCh <-chan singleflight.Result) ([]*domain.Alarm, error) {
	waitCtx := channelSubscriberWaitContext(ctx)

	select {
	case <-waitCtx.Done():
		return nil, fmt.Errorf("load channel subscriber alarms: wait for shared query: %w", waitCtx.Err())
	case result := <-resultCh:
		return resolveChannelSubscriberLoadResult(result)
	}
}

func channelSubscriberWaitContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
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
