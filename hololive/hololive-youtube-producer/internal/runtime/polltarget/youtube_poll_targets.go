package polltarget

import (
	"context"
	"fmt"
	"log/slog"

	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
)

type Targets struct {
	NotificationChannelIDs []string
	OperationalChannelIDs  []string
	DroppedAlarmTargets    int
}

var loadAlarmChannelIDsFromRepository = loadAlarmChannelIDs

func resolveYouTubePollTargets(
	ctx context.Context,
	cacheService cache.Client,
	postgresService database.Client,
	operationalChannels []communityshorts.OperationalChannel,
	logger *slog.Logger,
) (Targets, error) {
	alarmChannelIDs, err := loadAlarmChannelIDsFromRepository(ctx, postgresService)
	if err != nil {
		return Targets{}, err
	}
	dbTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(alarmChannelIDs, operationalChannels)

	if cacheService != nil {
		cacheChannelIDs, cacheErr := cacheService.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
		if cacheErr != nil {
			if logger != nil {
				logger.Warn("Failed to inspect cache-backed YouTube poll targets at startup",
					slog.Any("error", cacheErr),
				)
			}
			return dbTargets, nil
		}

		cacheTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(cacheChannelIDs, operationalChannels)
		logYouTubePollTargetStartupSourceState(logger, cacheTargets, dbTargets)
	}

	return dbTargets, nil
}

func loadAlarmChannelIDs(ctx context.Context, postgresService database.Client) ([]string, error) {
	reader := sharedproviders.ProvideAlarmReader(postgresService, nil)
	alarmChannelIDs, err := reader.GetAllChannelIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve youtube poll targets: get alarm channel ids: %w", err)
	}
	return alarmChannelIDs, nil
}

func resolveYouTubePollTargetsFromAlarmChannelIDs(
	alarmChannelIDs []string,
	operationalChannels []communityshorts.OperationalChannel,
) Targets {
	operationalChannelIDs := communityshorts.EnabledChannelIDs(operationalChannels)
	allowed := make(map[string]struct{}, len(operationalChannelIDs))
	for _, channelID := range operationalChannelIDs {
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

	return Targets{
		NotificationChannelIDs: notificationChannelIDs,
		OperationalChannelIDs:  operationalChannelIDs,
		DroppedAlarmTargets:    dropped,
	}
}

func logYouTubePollTargetStartupSourceState(
	logger *slog.Logger,
	cacheTargets Targets,
	dbTargets Targets,
) {
	if logger == nil {
		return
	}

	cacheOnly := diffChannelIDs(cacheTargets.NotificationChannelIDs, dbTargets.NotificationChannelIDs)
	dbOnly := diffChannelIDs(dbTargets.NotificationChannelIDs, cacheTargets.NotificationChannelIDs)
	if len(cacheOnly) == 0 && len(dbOnly) == 0 {
		logger.Info("youtube_poll_targets_startup_source_aligned",
			slog.Int("notification_target_channels", len(dbTargets.NotificationChannelIDs)),
			slog.Int("operational_target_channels", len(dbTargets.OperationalChannelIDs)),
		)
		return
	}

	logger.Warn("youtube_poll_targets_startup_source_diverged",
		slog.Int("db_notification_target_channels", len(dbTargets.NotificationChannelIDs)),
		slog.Int("cache_notification_target_channels", len(cacheTargets.NotificationChannelIDs)),
		slog.Int("cache_only_notification_channels", len(cacheOnly)),
		slog.Int("db_only_notification_channels", len(dbOnly)),
	)
}

func diffChannelIDs(left, right []string) []string {
	rightSet := buildChannelIDSet(right)
	out := make([]string, 0)
	seen := make(map[string]struct{}, len(left))
	for _, channelID := range left {
		if includeDiffChannelID(channelID, seen, rightSet) {
			out = append(out, channelID)
		}
	}
	return out
}

func mergeUniqueChannelIDs(channelIDSets ...[]string) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, channelIDs := range channelIDSets {
		for _, channelID := range channelIDs {
			merged = appendUniqueChannelID(merged, seen, channelID)
		}
	}
	return merged
}

func buildChannelIDSet(channelIDs []string) map[string]struct{} {
	set := make(map[string]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID != "" {
			set[channelID] = struct{}{}
		}
	}
	return set
}

func includeDiffChannelID(channelID string, seen, rightSet map[string]struct{}) bool {
	if channelID == "" {
		return false
	}
	if _, exists := seen[channelID]; exists {
		return false
	}
	seen[channelID] = struct{}{}
	_, exists := rightSet[channelID]
	return !exists
}

func appendUniqueChannelID(merged []string, seen map[string]struct{}, channelID string) []string {
	if channelID == "" {
		return merged
	}
	if _, exists := seen[channelID]; exists {
		return merged
	}
	seen[channelID] = struct{}{}
	return append(merged, channelID)
}
