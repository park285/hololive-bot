package runtime

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func buildCommunityShortsTargetBaselinePaths(
	channels []CommunityShortsTargetBaselineChannel,
	finalOwner string,
	cutoverPending bool,
) []CommunityShortsTargetBaselinePath {
	paths := make([]CommunityShortsTargetBaselinePath, 0, len(communityShortsTargetAlarmTypes()))
	for _, alarmType := range communityShortsTargetAlarmTypes() {
		configuredChannelCount := len(channels)
		alarmEnabledChannelCount := 0
		alarmEnabledRoomCount := 0
		pathCutoverPending := false
		for i := range channels {
			route, ok := communityShortsRouteForType(channels[i].Routes, alarmType)
			if !ok || !route.AlarmEnabled {
				continue
			}
			alarmEnabledChannelCount++
			alarmEnabledRoomCount += route.SubscriberRoomCount
			if route.CutoverPending {
				pathCutoverPending = true
			}
		}

		paths = append(paths, CommunityShortsTargetBaselinePath{
			AlarmType:                alarmType,
			LegacyDeliveryPath:       communityShortsLegacyDeliveryPath,
			LegacyStatus:             communityShortsLegacyStatus,
			LegacyPathActive:         false,
			NewDeliveryPath:          communityShortsNewDeliveryPath,
			NewPathConfigured:        true,
			CutoverPending:           cutoverPending && pathCutoverPending,
			FinalDeliveryOwner:       finalOwner,
			FinalDeliveryPath:        communityShortsFinalDeliveryPath(finalOwner),
			SubscriberKeyPrefix:      communityShortsSubscriberKeyPrefix(alarmType),
			ConfiguredChannelCount:   configuredChannelCount,
			AlarmEnabledChannelCount: alarmEnabledChannelCount,
			AlarmEnabledRoomCount:    alarmEnabledRoomCount,
			ActivationSource:         "postgres.alarms alarm_types",
		})
	}
	return paths
}

func buildCommunityShortsTargetBaselineRoutes(
	channelID string,
	finalOwner string,
	activationIndex map[communityShortsAlarmActivationKey]map[string]struct{},
	cutoverPending bool,
) []CommunityShortsTargetBaselineChannelRoute {
	routes := make([]CommunityShortsTargetBaselineChannelRoute, 0, len(communityShortsTargetAlarmTypes()))
	for _, alarmType := range communityShortsTargetAlarmTypes() {
		roomCount := lookupCommunityShortsAlarmRoomCount(activationIndex, channelID, alarmType)
		routeCutoverPending := cutoverPending && roomCount > 0
		routes = append(routes, CommunityShortsTargetBaselineChannelRoute{
			AlarmType:             alarmType,
			SubscriberKey:         sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType),
			AlarmEnabled:          roomCount > 0,
			SubscriberRoomCount:   roomCount,
			LegacyPathActive:      false,
			NewPathConfigured:     true,
			CutoverPending:        routeCutoverPending,
			EffectiveDeliveryMode: communityShortsEffectiveDeliveryMode(roomCount, routeCutoverPending),
			FinalDeliveryOwner:    finalOwner,
			FinalDeliveryPath:     communityShortsFinalDeliveryPath(finalOwner),
		})
	}
	return routes
}

func buildCommunityShortsAlarmActivationIndex(
	alarms []*domain.Alarm,
) map[communityShortsAlarmActivationKey]map[string]struct{} {
	index := make(map[communityShortsAlarmActivationKey]map[string]struct{})
	for _, alarmRecord := range alarms {
		if alarmRecord == nil {
			continue
		}

		roomID := strings.TrimSpace(alarmRecord.RoomID)
		channelID := strings.TrimSpace(alarmRecord.ChannelID)
		if roomID == "" || channelID == "" {
			continue
		}

		for _, alarmType := range normalizedCommunityShortsAlarmTypes(alarmRecord.AlarmTypes) {
			key := communityShortsAlarmActivationKey{channelID: channelID, alarmType: alarmType}
			roomSet := index[key]
			if roomSet == nil {
				roomSet = make(map[string]struct{})
				index[key] = roomSet
			}
			roomSet[alarmRecord.RegistryKey()] = struct{}{}
		}
	}
	return index
}

func normalizedCommunityShortsAlarmTypes(alarmTypes domain.AlarmTypes) []domain.AlarmType {
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

func communityShortsTargetAlarmTypes() []domain.AlarmType {
	return []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts}
}

func communityShortsSubscriberKeyPrefix(alarmType domain.AlarmType) string {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return sharedalarmkeys.ChannelSubscribersCommunityPrefix
	case domain.AlarmTypeShorts:
		return sharedalarmkeys.ChannelSubscribersShortsPrefix
	default:
		return ""
	}
}

func communityShortsRouteForType(
	routes []CommunityShortsTargetBaselineChannelRoute,
	alarmType domain.AlarmType,
) (CommunityShortsTargetBaselineChannelRoute, bool) {
	for i := range routes {
		if routes[i].AlarmType == alarmType {
			return routes[i], true
		}
	}
	return CommunityShortsTargetBaselineChannelRoute{}, false
}

func CommunityShortsRouteForType(
	routes []CommunityShortsTargetBaselineChannelRoute,
	alarmType domain.AlarmType,
) (CommunityShortsTargetBaselineChannelRoute, bool) {
	return communityShortsRouteForType(routes, alarmType)
}

func lookupCommunityShortsAlarmRoomCount(
	activationIndex map[communityShortsAlarmActivationKey]map[string]struct{},
	channelID string,
	alarmType domain.AlarmType,
) int {
	return len(activationIndex[communityShortsAlarmActivationKey{
		channelID: strings.TrimSpace(channelID),
		alarmType: alarmType,
	}])
}

func communityShortsEffectiveDeliveryMode(roomCount int, cutoverPending bool) string {
	if roomCount == 0 {
		return communityShortsDeliveryModeOff
	}
	if cutoverPending {
		return communityShortsDeliveryModePending
	}
	return communityShortsDeliveryModeNew
}

func communityShortsCutoverPending(ingestionCfg config.IngestionConfig, generatedAt time.Time) bool {
	if !ingestionCfg.CommunityShortsBigBangEnabled {
		return false
	}
	cutoverAt := normalizedCommunityShortsCutoverAt(ingestionCfg.CommunityShortsBigBangCutoverAt)
	if cutoverAt == nil {
		return false
	}
	return generatedAt.UTC().Before(*cutoverAt)
}

func resolveCommunityShortsFinalDeliveryOwner(ingestionCfg config.IngestionConfig) string {
	if ingestionCfg.CommunityShortsBigBangEnabled {
		return youtubeScraperRuntimeName
	}
	return streamIngesterRuntimeName
}

func normalizedCommunityShortsCutoverAt(cutoverAt time.Time) *time.Time {
	if cutoverAt.IsZero() {
		return nil
	}
	normalized := cutoverAt.UTC()
	return &normalized
}

func communityShortsFinalDeliveryPath(finalOwner string) string {
	trimmedOwner := strings.TrimSpace(finalOwner)
	if trimmedOwner == "" {
		return communityShortsNewDeliveryPath
	}
	return trimmedOwner + "." + communityShortsNewDeliveryPath
}
