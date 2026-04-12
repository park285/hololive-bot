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

var channelSubscriberLoadGroup singleflight.Group

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
		subscribers, err := LookupChannelSubscribersByType(ctx, cacheSvc, normalizedChannelID, alarmType)
		if err == nil {
			normalizedSubscribers := normalizeSubscriberIDs(subscribers)
			if len(normalizedSubscribers) > 0 {
				return normalizedSubscribers, nil
			}
			isKnownEmpty, existsErr := cacheSvc.Exists(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(normalizedChannelID, alarmType))
			if existsErr == nil && isKnownEmpty {
				return nil, nil
			}
			if existsErr != nil && db == nil {
				return nil, fmt.Errorf("resolve channel subscribers by type: check empty subscriber cache: %w", existsErr)
			}
		} else if db == nil {
			return nil, fmt.Errorf("resolve channel subscribers by type: %w", err)
		}
	}

	alarms, err := loadChannelSubscriberAlarms(ctx, db, normalizedChannelID)
	if err != nil {
		observeAlarmSubscriberDBFallback("error")
		return nil, fmt.Errorf("resolve channel subscribers by type: %w", err)
	}

	subscribers := extractSubscriberIDsByType(alarms, alarmType)
	if len(subscribers) == 0 {
		observeAlarmSubscriberDBFallback("miss")
		if cacheSvc != nil {
			_ = cacheSvc.Set(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(normalizedChannelID, alarmType), "1", emptyChannelSubscriberCacheTTL)
		}
		return nil, nil
	}
	observeAlarmSubscriberDBFallback("hit")

	if cacheSvc != nil {
		_, _ = WarmSubscriberCacheFromAlarms(ctx, cacheSvc, alarms)
		_ = cacheSvc.Del(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(normalizedChannelID, alarmType))
	}

	return subscribers, nil
}

func loadChannelSubscriberAlarms(ctx context.Context, db *gorm.DB, channelID string) ([]*domain.Alarm, error) {
	if db == nil {
		return nil, errors.New("load channel subscriber alarms: database is nil")
	}

	normalizedChannelID := strings.TrimSpace(channelID)
	waitCtx := ctx
	if waitCtx == nil {
		waitCtx = context.Background()
	}

	resultCh := channelSubscriberLoadGroup.DoChan(normalizedChannelID, func() (any, error) {
		queryCtx := context.Background()
		if ctx != nil {
			queryCtx = context.WithoutCancel(ctx)
		}

		var records []domain.Alarm
		if err := db.WithContext(queryCtx).
			Where("channel_id = ?", normalizedChannelID).
			Order("created_at ASC").
			Find(&records).Error; err != nil {
			return nil, fmt.Errorf("load channel subscriber alarms: %w", err)
		}

		alarms := make([]*domain.Alarm, 0, len(records))
		for i := range records {
			alarms = append(alarms, &records[i])
		}

		return alarms, nil
	})

	select {
	case <-waitCtx.Done():
		return nil, fmt.Errorf("load channel subscriber alarms: wait for shared query: %w", waitCtx.Err())
	case result := <-resultCh:
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
		if alarmRecord == nil {
			continue
		}

		alarmTypes := alarmRecord.AlarmTypes
		if len(alarmTypes) == 0 {
			alarmTypes = domain.DefaultAlarmTypes
		}
		if !alarmTypes.Contains(alarmType) {
			continue
		}

		subscriberID := strings.TrimSpace(alarmRecord.RegistryKey())
		if subscriberID == "" {
			continue
		}
		if _, exists := seen[subscriberID]; exists {
			continue
		}

		seen[subscriberID] = struct{}{}
		subscribers = append(subscribers, subscriberID)
	}

	return subscribers
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
