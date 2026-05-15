package communityshorts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func TestBuildTargetBaseline(t *testing.T) {
	t.Parallel()

	t.Run("build operational channels deduplicates shared channel ids", func(t *testing.T) {
		t.Parallel()
		channels := BuildOperationalChannelsFromMembers([]*domain.Member{
			{Name: "Fuwawa Abyssgard", Org: "Hololive", ChannelID: "UCt9H_RpQzhxzlyBxFqrdHqA"},
			{Name: "Mococo Abyssgard", Org: "Hololive", ChannelID: "UCt9H_RpQzhxzlyBxFqrdHqA"},
			{Name: "Miko", Org: "Hololive", ChannelID: "UCmiko"},
		})
		require.Len(t, channels, 2)
		require.Equal(t, "Fuwawa Abyssgard (Hololive)", channels[0].OwnerLabel)
		require.Equal(t, "UCt9H_RpQzhxzlyBxFqrdHqA", channels[0].ChannelID)
		require.Equal(t, "UCmiko", channels[1].ChannelID)
	})

	t.Run("collects sorted enabled channels and exposes new-only activation state", func(t *testing.T) {
		t.Parallel()
		cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
		baseline, err := BuildTargetBaseline([]OperationalChannel{
			{OwnerLabel: "Miko", ChannelID: "UCmiko", Enabled: true},
			{OwnerLabel: "Pekora", ChannelID: " UCpekora ", Enabled: true},
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

		require.Equal(t, RuntimeOwnerAlarmWorker, baseline.Runtime.FinalDeliveryOwner)
		require.Equal(t, 2, baseline.Runtime.TargetChannelCount)
		require.NotNil(t, baseline.Runtime.CommunityShortsBigBangCutoverAt)
		require.Equal(t, cutoverAt, *baseline.Runtime.CommunityShortsBigBangCutoverAt)

		communityPath := baselinePathForType(t, baseline.PathMappings, domain.AlarmTypeCommunity)
		require.Equal(t, sharedalarmkeys.ChannelSubscribersCommunityPrefix, communityPath.SubscriberKeyPrefix)
		require.Equal(t, RuntimeOwnerAlarmWorker+"."+NewDeliveryPath, communityPath.FinalDeliveryPath)
		require.False(t, communityPath.LegacyPathActive)
		require.True(t, communityPath.NewPathConfigured)
		require.False(t, communityPath.CutoverPending)
		require.Equal(t, 2, communityPath.ConfiguredChannelCount)
		require.Equal(t, 1, communityPath.AlarmEnabledChannelCount)
		require.Equal(t, 2, communityPath.AlarmEnabledRoomCount)

		require.Len(t, baseline.Channels, 2)
		require.Equal(t, "UCmiko", baseline.Channels[0].ChannelID)
		mikoCommunity, _ := RouteForType(baseline.Channels[0].Routes, domain.AlarmTypeCommunity)
		require.True(t, mikoCommunity.AlarmEnabled)
		require.Equal(t, 2, mikoCommunity.SubscriberRoomCount)
		require.Equal(t, DeliveryModeNew, mikoCommunity.EffectiveDeliveryMode)
	})

	t.Run("reports pending cutover before activation time", func(t *testing.T) {
		t.Parallel()
		cutoverAt := time.Date(2026, 4, 10, 3, 0, 0, 0, time.UTC)
		generatedAt := cutoverAt.Add(-30 * time.Minute)
		baseline, err := BuildTargetBaseline([]OperationalChannel{{OwnerLabel: "Miko", ChannelID: "UCmiko", Enabled: true}}, []*domain.Alarm{{
			RoomID: "room-community-1", ChannelID: "UCmiko", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
		}}, config.IngestionConfig{
			CommunityShortsBigBangEnabled:   true,
			CommunityShortsBigBangCutoverAt: cutoverAt,
		}, generatedAt)
		require.NoError(t, err)
		communityRoute, _ := RouteForType(baseline.Channels[0].Routes, domain.AlarmTypeCommunity)
		require.True(t, communityRoute.CutoverPending)
		require.Equal(t, DeliveryModePending, communityRoute.EffectiveDeliveryMode)
	})

	t.Run("falls back to stream ingester owner before big bang", func(t *testing.T) {
		t.Parallel()
		baseline, err := BuildTargetBaseline([]OperationalChannel{{OwnerLabel: "Sora", ChannelID: "UCsora", Enabled: true}}, nil, config.IngestionConfig{}, time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.Equal(t, RuntimeOwnerStreamIngester, baseline.Runtime.FinalDeliveryOwner)
	})
}

func baselinePathForType(t *testing.T, paths []TargetBaselinePath, alarmType domain.AlarmType) TargetBaselinePath {
	t.Helper()
	for i := range paths {
		if paths[i].AlarmType == alarmType {
			return paths[i]
		}
	}
	t.Fatalf("path mapping for %s not found", alarmType)
	return TargetBaselinePath{}
}
