package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func TestBuildCommunityShortsTargetBaseline(t *testing.T) {
	t.Parallel()

	t.Run("collects sorted enabled channels and exposes new-only activation state", func(t *testing.T) {
		t.Parallel()

		cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
		baseline, err := buildCommunityShortsTargetBaseline([]communityShortsOperationalChannel{
			{ownerLabel: "Miko", channelID: "UCmiko", enabled: true},
			{ownerLabel: "Pekora", channelID: " UCpekora ", enabled: true},
		}, []*domain.Alarm{
			{RoomID: "room-community-1", ChannelID: "UCmiko", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity}},
			{RoomID: "room-default-1", ChannelID: "UCmiko"},
			{RoomID: "room-shorts-1", ChannelID: "UCpekora", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts}},
			{RoomID: "room-live-only", ChannelID: "UCpekora", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		}, config.IngestionConfig{
			CommunityShortsBigBangEnabled:   true,
			CommunityShortsBigBangCutoverAt: cutoverAt,
		}, cutoverAt.Add(time.Minute))
		require.NoError(t, err)

		require.Equal(t, youtubeScraperRuntimeName, baseline.Runtime.FinalDeliveryOwner)
		require.Equal(t, 2, baseline.Runtime.TargetChannelCount)
		require.NotNil(t, baseline.Runtime.CommunityShortsBigBangCutoverAt)
		require.Equal(t, cutoverAt, *baseline.Runtime.CommunityShortsBigBangCutoverAt)
		require.Equal(t, "postgres.alarms alarm_types -> community/shorts typed room counts", baseline.Sources.RoomSubscriptions)

		communityPath := baselinePathForType(t, baseline.PathMappings, domain.AlarmTypeCommunity)
		require.Equal(t, sharedalarmkeys.ChannelSubscribersCommunityPrefix, communityPath.SubscriberKeyPrefix)
		require.Equal(t, youtubeScraperRuntimeName+"."+communityShortsNewDeliveryPath, communityPath.FinalDeliveryPath)
		require.False(t, communityPath.LegacyPathActive)
		require.True(t, communityPath.NewPathConfigured)
		require.False(t, communityPath.CutoverPending)
		require.Equal(t, 2, communityPath.ConfiguredChannelCount)
		require.Equal(t, 1, communityPath.AlarmEnabledChannelCount)
		require.Equal(t, 2, communityPath.AlarmEnabledRoomCount)

		shortsPath := baselinePathForType(t, baseline.PathMappings, domain.AlarmTypeShorts)
		require.Equal(t, sharedalarmkeys.ChannelSubscribersShortsPrefix, shortsPath.SubscriberKeyPrefix)
		require.False(t, shortsPath.LegacyPathActive)
		require.True(t, shortsPath.NewPathConfigured)
		require.False(t, shortsPath.CutoverPending)
		require.Equal(t, 2, shortsPath.ConfiguredChannelCount)
		require.Equal(t, 2, shortsPath.AlarmEnabledChannelCount)
		require.Equal(t, 2, shortsPath.AlarmEnabledRoomCount)

		require.Len(t, baseline.Channels, 2)
		require.Equal(t, "UCmiko", baseline.Channels[0].ChannelID)
		require.Equal(t, sharedalarmkeys.ChannelSubscribersCommunityPrefix+"UCmiko", baseline.Channels[0].CommunitySubscribersKey)
		require.Equal(t, sharedalarmkeys.ChannelSubscribersShortsPrefix+"UCmiko", baseline.Channels[0].ShortsSubscribersKey)

		mikoCommunity := baselineRouteForType(t, baseline.Channels[0].Routes, domain.AlarmTypeCommunity)
		require.Equal(t, sharedalarmkeys.ChannelSubscribersCommunityPrefix+"UCmiko", mikoCommunity.SubscriberKey)
		require.True(t, mikoCommunity.AlarmEnabled)
		require.Equal(t, 2, mikoCommunity.SubscriberRoomCount)
		require.False(t, mikoCommunity.LegacyPathActive)
		require.True(t, mikoCommunity.NewPathConfigured)
		require.False(t, mikoCommunity.CutoverPending)
		require.Equal(t, communityShortsDeliveryModeNew, mikoCommunity.EffectiveDeliveryMode)

		mikoShorts := baselineRouteForType(t, baseline.Channels[0].Routes, domain.AlarmTypeShorts)
		require.True(t, mikoShorts.AlarmEnabled)
		require.Equal(t, 1, mikoShorts.SubscriberRoomCount)
		require.False(t, mikoShorts.CutoverPending)
		require.Equal(t, communityShortsDeliveryModeNew, mikoShorts.EffectiveDeliveryMode)

		require.Equal(t, "UCpekora", baseline.Channels[1].ChannelID)
		pekoraCommunity := baselineRouteForType(t, baseline.Channels[1].Routes, domain.AlarmTypeCommunity)
		require.False(t, pekoraCommunity.AlarmEnabled)
		require.Zero(t, pekoraCommunity.SubscriberRoomCount)
		require.False(t, pekoraCommunity.CutoverPending)
		require.Equal(t, communityShortsDeliveryModeOff, pekoraCommunity.EffectiveDeliveryMode)

		pekoraShorts := baselineRouteForType(t, baseline.Channels[1].Routes, domain.AlarmTypeShorts)
		require.True(t, pekoraShorts.AlarmEnabled)
		require.Equal(t, 1, pekoraShorts.SubscriberRoomCount)
		require.False(t, pekoraShorts.CutoverPending)
		require.Equal(t, communityShortsDeliveryModeNew, pekoraShorts.EffectiveDeliveryMode)
	})

	t.Run("reports pending cutover instead of new-only before activation time", func(t *testing.T) {
		t.Parallel()

		cutoverAt := time.Date(2026, 4, 10, 3, 0, 0, 0, time.UTC)
		generatedAt := cutoverAt.Add(-30 * time.Minute)
		baseline, err := buildCommunityShortsTargetBaseline([]communityShortsOperationalChannel{
			{ownerLabel: "Miko", channelID: "UCmiko", enabled: true},
		}, []*domain.Alarm{{
			RoomID: "room-community-1", ChannelID: "UCmiko", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
		}}, config.IngestionConfig{
			CommunityShortsBigBangEnabled:   true,
			CommunityShortsBigBangCutoverAt: cutoverAt,
		}, generatedAt)
		require.NoError(t, err)

		communityPath := baselinePathForType(t, baseline.PathMappings, domain.AlarmTypeCommunity)
		require.True(t, communityPath.CutoverPending)

		communityRoute := baselineRouteForType(t, baseline.Channels[0].Routes, domain.AlarmTypeCommunity)
		require.True(t, communityRoute.AlarmEnabled)
		require.True(t, communityRoute.CutoverPending)
		require.Equal(t, communityShortsDeliveryModePending, communityRoute.EffectiveDeliveryMode)

		shortsRoute := baselineRouteForType(t, baseline.Channels[0].Routes, domain.AlarmTypeShorts)
		require.False(t, shortsRoute.AlarmEnabled)
		require.False(t, shortsRoute.CutoverPending)
		require.Equal(t, communityShortsDeliveryModeOff, shortsRoute.EffectiveDeliveryMode)
	})

	t.Run("falls back to stream ingester owner before big bang", func(t *testing.T) {
		t.Parallel()

		baseline, err := buildCommunityShortsTargetBaseline([]communityShortsOperationalChannel{
			{ownerLabel: "Sora", channelID: "UCsora", enabled: true},
		}, nil, config.IngestionConfig{}, time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.Equal(t, streamIngesterRuntimeName, baseline.Runtime.FinalDeliveryOwner)
		require.Nil(t, baseline.Runtime.CommunityShortsBigBangCutoverAt)

		communityPath := baselinePathForType(t, baseline.PathMappings, domain.AlarmTypeCommunity)
		require.Equal(t, streamIngesterRuntimeName+"."+communityShortsNewDeliveryPath, communityPath.FinalDeliveryPath)
		require.False(t, communityPath.LegacyPathActive)
		require.True(t, communityPath.NewPathConfigured)
		require.False(t, communityPath.CutoverPending)
		require.Zero(t, communityPath.AlarmEnabledChannelCount)
		require.Zero(t, communityPath.AlarmEnabledRoomCount)

		communityRoute := baselineRouteForType(t, baseline.Channels[0].Routes, domain.AlarmTypeCommunity)
		require.False(t, communityRoute.AlarmEnabled)
		require.False(t, communityRoute.CutoverPending)
		require.Equal(t, communityShortsDeliveryModeOff, communityRoute.EffectiveDeliveryMode)
	})

	t.Run("rejects missing operational target definitions", func(t *testing.T) {
		t.Parallel()

		_, err := buildCommunityShortsTargetBaseline([]communityShortsOperationalChannel{
			{ownerLabel: "Missing", channelID: "", enabled: false},
		}, nil, config.IngestionConfig{
			CommunityShortsBigBangEnabled: true,
		}, time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC))
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing operating channel targets")
	})
}

func baselinePathForType(
	t *testing.T,
	paths []CommunityShortsTargetBaselinePath,
	alarmType domain.AlarmType,
) CommunityShortsTargetBaselinePath {
	t.Helper()
	for i := range paths {
		if paths[i].AlarmType == alarmType {
			return paths[i]
		}
	}
	t.Fatalf("path mapping for %s not found", alarmType)
	return CommunityShortsTargetBaselinePath{}
}

func baselineRouteForType(
	t *testing.T,
	routes []CommunityShortsTargetBaselineChannelRoute,
	alarmType domain.AlarmType,
) CommunityShortsTargetBaselineChannelRoute {
	t.Helper()
	for i := range routes {
		if routes[i].AlarmType == alarmType {
			return routes[i]
		}
	}
	t.Fatalf("channel route for %s not found", alarmType)
	return CommunityShortsTargetBaselineChannelRoute{}
}
