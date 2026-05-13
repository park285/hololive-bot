package alarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

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
	cacheSvc cache.Client,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	if cacheSvc == nil {
		return nil, fmt.Errorf("lookup channel subscribers by type: cache service is nil")
	}

	normalizedChannelID := strings.TrimSpace(channelID)
	if normalizedChannelID == "" {
		return nil, nil
	}

	key := sharedalarmkeys.BuildChannelSubscriberKey(normalizedChannelID, alarmType)
	subscribers, err := cacheSvc.SMembers(ctx, key)
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
	cacheSvc cache.Client,
	db *gorm.DB,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	normalizedChannelID := strings.TrimSpace(channelID)
	if normalizedChannelID == "" {
		return nil, nil
	}

	if cacheSvc != nil {
		subscribers, resolved, err := resolveChannelSubscribersFromCache(ctx, cacheSvc, normalizedChannelID, alarmType, db == nil)
		if resolved || err != nil {
			return subscribers, err
		}
	}

	return resolveChannelSubscribersFromDB(ctx, cacheSvc, db, normalizedChannelID, alarmType)
}

func resolveChannelSubscribersFromCache(
	ctx context.Context,
	cacheSvc cache.Client,
	channelID string,
	alarmType domain.AlarmType,
	requireCacheSuccess bool,
) ([]string, bool, error) {
	subscribers, err := LookupChannelSubscribersByType(ctx, cacheSvc, channelID, alarmType)
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

	isKnownEmpty, err := cacheSvc.Exists(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType))
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
	cacheSvc cache.Client,
	db *gorm.DB,
	channelID string,
	alarmType domain.AlarmType,
) ([]string, error) {
	alarms, err := loadChannelSubscriberAlarms(ctx, db, channelID)
	if err != nil {
		observeAlarmSubscriberDBFallback("error")
		return nil, fmt.Errorf("resolve channel subscribers by type: %w", err)
	}

	subscribers := extractSubscriberIDsByType(alarms, alarmType)
	if len(subscribers) == 0 {
		observeAlarmSubscriberDBFallback("miss")
		markEmptyChannelSubscriberCache(ctx, cacheSvc, channelID, alarmType)
		return nil, nil
	}
	observeAlarmSubscriberDBFallback("hit")

	warmChannelSubscriberCache(ctx, cacheSvc, alarms, channelID, alarmType)

	return subscribers, nil
}

func markEmptyChannelSubscriberCache(ctx context.Context, cacheSvc cache.Client, channelID string, alarmType domain.AlarmType) {
	if cacheSvc == nil {
		return
	}
	_ = cacheSvc.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType), "1", emptyChannelSubscriberCacheTTL)
}

func warmChannelSubscriberCache(ctx context.Context, cacheSvc cache.Client, alarms []*domain.Alarm, channelID string, alarmType domain.AlarmType) {
	if cacheSvc == nil {
		return
	}
	_, _ = WarmSubscriberCacheFromAlarms(ctx, cacheSvc, alarms)
	_ = cacheSvc.Del(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType))
}

func loadChannelSubscriberAlarms(ctx context.Context, db *gorm.DB, channelID string) ([]*domain.Alarm, error) {
	if db == nil {
		return nil, errors.New("load channel subscriber alarms: database is nil")
	}

	normalizedChannelID := strings.TrimSpace(channelID)
	resultCh := channelSubscriberLoadGroup.DoChan(normalizedChannelID, func() (any, error) {
		return queryChannelSubscriberAlarms(ctx, db, normalizedChannelID)
	})

	return waitForChannelSubscriberAlarms(ctx, resultCh)
}

func queryChannelSubscriberAlarms(ctx context.Context, db *gorm.DB, channelID string) ([]*domain.Alarm, error) {
	queryCtx, cancel := withoutCancelPreserveDeadline(ctx, channelSubscriberLoadTimeout)
	defer cancel()

	var records []domain.Alarm
	if err := db.WithContext(queryCtx).
		Where("channel_id = ?", channelID).
		Order("created_at ASC").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load channel subscriber alarms: %w", err)
	}

	alarms := make([]*domain.Alarm, 0, len(records))
	for i := range records {
		alarms = append(alarms, &records[i])
	}

	return alarms, nil
}

func waitForChannelSubscriberAlarms(ctx context.Context, resultCh <-chan singleflight.Result) ([]*domain.Alarm, error) {
	waitCtx := ctx
	if waitCtx == nil {
		waitCtx = context.Background()
	}

	select {
	case <-waitCtx.Done():
		return nil, fmt.Errorf("load channel subscriber alarms: wait for shared query: %w", waitCtx.Err())
	case result := <-resultCh:
		return resolveChannelSubscriberLoadResult(result)
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
