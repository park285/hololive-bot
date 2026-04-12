package app

import (
	"context"
	"fmt"

	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type youtubePollTargets struct {
	NotificationChannelIDs []string
	StatsChannelIDs        []string
	DroppedAlarmTargets    int
}

var loadAlarmChannelIDsFromRepository = loadAlarmChannelIDs

func resolveYouTubePollTargets(
	ctx context.Context,
	cacheService cache.Client,
	postgresService database.Client,
	operationalChannels []communityShortsOperationalChannel,
) (youtubePollTargets, error) {
	if cacheService != nil {
		cacheChannelIDs, err := cacheService.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
		if err == nil && len(cacheChannelIDs) > 0 {
			cacheTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(cacheChannelIDs, operationalChannels)

			dbAlarmChannelIDs, err := loadAlarmChannelIDsFromRepository(ctx, postgresService)
			if err != nil {
				return youtubePollTargets{}, fmt.Errorf("resolve youtube poll targets: validate cache targets from db: %w", err)
			}
			dbTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(dbAlarmChannelIDs, operationalChannels)
			if shouldValidateTargetShrink(dbTargets, cacheTargets) {
				return dbTargets, nil
			}

			return cacheTargets, nil
		}
	}

	alarmChannelIDs, err := loadAlarmChannelIDsFromRepository(ctx, postgresService)
	if err != nil {
		return youtubePollTargets{}, err
	}
	return resolveYouTubePollTargetsFromAlarmChannelIDs(alarmChannelIDs, operationalChannels), nil
}

func loadAlarmChannelIDs(ctx context.Context, postgresService database.Client) ([]string, error) {
	repo := sharedalarm.NewRepository(postgresService, nil)
	alarmChannelIDs, err := repo.GetAllChannelIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve youtube poll targets: get alarm channel ids: %w", err)
	}
	return alarmChannelIDs, nil
}

func resolveYouTubePollTargetsFromAlarmChannelIDs(
	alarmChannelIDs []string,
	operationalChannels []communityShortsOperationalChannel,
) youtubePollTargets {
	statsChannelIDs := communityShortsEnabledChannelIDs(operationalChannels)
	allowed := make(map[string]struct{}, len(statsChannelIDs))
	for _, channelID := range statsChannelIDs {
		allowed[channelID] = struct{}{}
	}

	notificationChannelIDs := make([]string, 0, len(alarmChannelIDs))
	seen := make(map[string]struct{}, len(alarmChannelIDs))
	dropped := 0
	for _, channelID := range alarmChannelIDs {
		if _, exists := seen[channelID]; exists {
			continue
		}
		seen[channelID] = struct{}{}
		if _, ok := allowed[channelID]; !ok {
			dropped++
			continue
		}
		notificationChannelIDs = append(notificationChannelIDs, channelID)
	}

	return youtubePollTargets{
		NotificationChannelIDs: notificationChannelIDs,
		StatsChannelIDs:        statsChannelIDs,
		DroppedAlarmTargets:    dropped,
	}
}

func mergeUniqueChannelIDs(channelIDSets ...[]string) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, channelIDs := range channelIDSets {
		for _, channelID := range channelIDs {
			if channelID == "" {
				continue
			}
			if _, exists := seen[channelID]; exists {
				continue
			}
			seen[channelID] = struct{}{}
			merged = append(merged, channelID)
		}
	}
	return merged
}
