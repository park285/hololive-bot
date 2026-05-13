package communityshorts

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func buildTargetBaselinePaths(
	channels []TargetBaselineChannel,
	finalOwner string,
	cutoverPending bool,
) []TargetBaselinePath {
	paths := make([]TargetBaselinePath, 0, len(targetAlarmTypes()))
	for _, alarmType := range targetAlarmTypes() {
		alarmEnabledChannelCount, alarmEnabledRoomCount, pathCutoverPending := targetBaselinePathCounts(channels, alarmType)

		paths = append(paths, TargetBaselinePath{
			AlarmType:                alarmType,
			LegacyDeliveryPath:       LegacyDeliveryPath,
			LegacyStatus:             LegacyStatus,
			LegacyPathActive:         false,
			NewDeliveryPath:          NewDeliveryPath,
			NewPathConfigured:        true,
			CutoverPending:           cutoverPending && pathCutoverPending,
			FinalDeliveryOwner:       finalOwner,
			FinalDeliveryPath:        finalDeliveryPath(finalOwner),
			SubscriberKeyPrefix:      subscriberKeyPrefix(alarmType),
			ConfiguredChannelCount:   len(channels),
			AlarmEnabledChannelCount: alarmEnabledChannelCount,
			AlarmEnabledRoomCount:    alarmEnabledRoomCount,
			ActivationSource:         "postgres.alarms alarm_types",
		})
	}
	return paths
}

func targetBaselinePathCounts(channels []TargetBaselineChannel, alarmType domain.AlarmType) (int, int, bool) {
	alarmEnabledChannelCount := 0
	alarmEnabledRoomCount := 0
	pathCutoverPending := false
	for i := range channels {
		route, ok := RouteForType(channels[i].Routes, alarmType)
		if !ok || !route.AlarmEnabled {
			continue
		}
		alarmEnabledChannelCount++
		alarmEnabledRoomCount += route.SubscriberRoomCount
		if route.CutoverPending {
			pathCutoverPending = true
		}
	}
	return alarmEnabledChannelCount, alarmEnabledRoomCount, pathCutoverPending
}

func buildTargetBaselineRoutes(
	channelID string,
	finalOwner string,
	activationIndex map[alarmActivationKey]map[string]struct{},
	cutoverPending bool,
) []TargetBaselineChannelRoute {
	routes := make([]TargetBaselineChannelRoute, 0, len(targetAlarmTypes()))
	for _, alarmType := range targetAlarmTypes() {
		roomCount := lookupAlarmRoomCount(activationIndex, channelID, alarmType)
		routeCutoverPending := cutoverPending && roomCount > 0
		routes = append(routes, TargetBaselineChannelRoute{
			AlarmType:             alarmType,
			SubscriberKey:         sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType),
			AlarmEnabled:          roomCount > 0,
			SubscriberRoomCount:   roomCount,
			LegacyPathActive:      false,
			NewPathConfigured:     true,
			CutoverPending:        routeCutoverPending,
			EffectiveDeliveryMode: effectiveDeliveryMode(roomCount, routeCutoverPending),
			FinalDeliveryOwner:    finalOwner,
			FinalDeliveryPath:     finalDeliveryPath(finalOwner),
		})
	}
	return routes
}

func normalizedAlarmTypes(alarmTypes domain.AlarmTypes) []domain.AlarmType {
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}

	result := make([]domain.AlarmType, 0, len(alarmTypes))
	seen := make(map[domain.AlarmType]struct{}, len(alarmTypes))
	for _, alarmType := range alarmTypes {
		if alarmType != domain.AlarmTypeCommunity && alarmType != domain.AlarmTypeShorts {
			continue
		}
		if _, ok := seen[alarmType]; ok {
			continue
		}
		seen[alarmType] = struct{}{}
		result = append(result, alarmType)
	}
	return result
}

func targetAlarmTypes() []domain.AlarmType {
	return []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts}
}

func subscriberKeyPrefix(alarmType domain.AlarmType) string {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return sharedalarmkeys.ChannelSubscribersCommunityPrefix
	case domain.AlarmTypeShorts:
		return sharedalarmkeys.ChannelSubscribersShortsPrefix
	default:
		return ""
	}
}

func RouteForType(routes []TargetBaselineChannelRoute, alarmType domain.AlarmType) (TargetBaselineChannelRoute, bool) {
	for i := range routes {
		if routes[i].AlarmType == alarmType {
			return routes[i], true
		}
	}
	return TargetBaselineChannelRoute{}, false
}

func lookupAlarmRoomCount(activationIndex map[alarmActivationKey]map[string]struct{}, channelID string, alarmType domain.AlarmType) int {
	return len(activationIndex[alarmActivationKey{
		channelID: strings.TrimSpace(channelID),
		alarmType: alarmType,
	}])
}

func effectiveDeliveryMode(roomCount int, cutoverPending bool) string {
	if roomCount == 0 {
		return DeliveryModeOff
	}
	if cutoverPending {
		return DeliveryModePending
	}
	return DeliveryModeNew
}

func isCutoverPending(ingestionCfg config.IngestionConfig, generatedAt time.Time) bool {
	if !ingestionCfg.CommunityShortsBigBangEnabled {
		return false
	}
	cutoverAt := normalizedCutoverAt(ingestionCfg.CommunityShortsBigBangCutoverAt)
	if cutoverAt == nil {
		return false
	}
	return generatedAt.UTC().Before(*cutoverAt)
}

func resolveFinalDeliveryOwner(ingestionCfg config.IngestionConfig) string {
	if ingestionCfg.CommunityShortsBigBangEnabled {
		return RuntimeOwnerYouTubeScraper
	}
	return RuntimeOwnerStreamIngester
}

func normalizedCutoverAt(cutoverAt time.Time) *time.Time {
	if cutoverAt.IsZero() {
		return nil
	}
	normalized := cutoverAt.UTC()
	return &normalized
}

func finalDeliveryPath(finalOwner string) string {
	trimmedOwner := strings.TrimSpace(finalOwner)
	if trimmedOwner == "" {
		return NewDeliveryPath
	}
	return trimmedOwner + "." + NewDeliveryPath
}
