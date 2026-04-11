package app

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

func TestBuildCommunityShortsBigBangPolicy(t *testing.T) {
	t.Parallel()

	t.Run("collects active target channels and routes only content requests at or after cutover", func(t *testing.T) {
		t.Parallel()

		cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
		policy, err := buildCommunityShortsBigBangPolicy(config.IngestionConfig{
			CommunityShortsBigBangEnabled:   true,
			CommunityShortsBigBangCutoverAt: cutoverAt,
		}, mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
			members: []*domain.Member{
				{Name: "Pekora", Org: "Hololive", ChannelID: " UCpekora "},
				{Name: "Miko", Org: "Hololive", ChannelID: "UCmiko"},
				{Name: "Graduated", Org: "Hololive", ChannelID: "UCgraduated", IsGraduated: true},
			},
		}))
		require.NoError(t, err)
		assert.True(t, policy.Enabled())
		assert.Equal(t, cutoverAt, policy.CutoverAt())
		assert.Equal(t, 2, policy.TargetChannelCount())

		assert.True(t, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeCommunity,
			ChannelID:   "UCpekora",
			PublishedAt: cutoverAt,
		}))
		assert.True(t, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeShorts,
			ChannelID:   "UCmiko",
			PublishedAt: cutoverAt.Add(30 * time.Second),
		}))
		assert.False(t, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeCommunity,
			ChannelID:   "UCpekora",
			PublishedAt: cutoverAt.Add(-time.Second),
		}))
		assert.False(t, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeLive,
			ChannelID:   "UCpekora",
			PublishedAt: cutoverAt.Add(time.Second),
		}))
		assert.False(t, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeShorts,
			ChannelID:   "UCunknown",
			PublishedAt: cutoverAt.Add(time.Second),
		}))
		assert.False(t, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType: domain.AlarmTypeShorts,
			ChannelID: "UCmiko",
		}))
	})

	t.Run("keeps deployment targets and route selection aligned across rollout channel states", func(t *testing.T) {
		t.Parallel()

		cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
		tests := map[string]struct {
			channels func(*testing.T) []communityShortsOperationalChannel
		}{
			"deactivated channels stay on the legacy path": {
				channels: func(*testing.T) []communityShortsOperationalChannel {
					return []communityShortsOperationalChannel{
						{ownerLabel: "Enabled", channelID: "UCenabled", enabled: true},
						{ownerLabel: "Disabled", channelID: "UCdisabled", enabled: false},
					}
				},
			},
			"partial rollout only targets the enabled subset": {
				channels: func(*testing.T) []communityShortsOperationalChannel {
					return []communityShortsOperationalChannel{
						{ownerLabel: "First", channelID: "UCfirst", enabled: true},
						{ownerLabel: "Second", channelID: "UCsecond", enabled: false},
						{ownerLabel: "Third", channelID: "UCthird", enabled: true},
					}
				},
			},
			"all resolved operational channels remain included": {
				channels: func(t *testing.T) []communityShortsOperationalChannel {
					return mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
						members: []*domain.Member{
							{Name: "Pekora", Org: "Hololive", ChannelID: " UCpekora "},
							{Name: "Miko", Org: "Hololive", ChannelID: "UCmiko"},
							{Name: "Suisei", Org: "Hololive", ChannelID: "UCsuisei"},
							{Name: "Graduated", Org: "Hololive", ChannelID: "UCgraduated", IsGraduated: true},
						},
					})
				},
			},
		}

		for name, tc := range tests {
			tc := tc
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				channels := tc.channels(t)
				policy, err := buildCommunityShortsBigBangPolicy(config.IngestionConfig{
					CommunityShortsBigBangEnabled:   true,
					CommunityShortsBigBangCutoverAt: cutoverAt,
				}, channels)
				require.NoError(t, err)
				assert.True(t, policy.Enabled())
				assertCommunityShortsPolicyTargetsMatchEnabledChannels(t, policy, channels, cutoverAt)
			})
		}
	})

	t.Run("uses resolver enablement without reinterpreting channel ids", func(t *testing.T) {
		t.Parallel()

		cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
		policy, err := buildCommunityShortsBigBangPolicy(config.IngestionConfig{
			CommunityShortsBigBangEnabled:   true,
			CommunityShortsBigBangCutoverAt: cutoverAt,
		}, []communityShortsOperationalChannel{{
			ownerLabel: "Resolver-disabled",
			channelID:  "UCshadow",
			enabled:    false,
		}})
		require.NoError(t, err)
		assert.False(t, policy.Enabled())
		assert.Zero(t, policy.TargetChannelCount())
		assert.False(t, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeCommunity,
			ChannelID:   "UCshadow",
			PublishedAt: cutoverAt,
		}))
	})

	t.Run("stays inactive until a cutover timestamp is configured", func(t *testing.T) {
		t.Parallel()

		policy, err := buildCommunityShortsBigBangPolicy(config.IngestionConfig{
			CommunityShortsBigBangEnabled: true,
		}, mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
			members: []*domain.Member{{Name: "Pekora", Org: "Hololive", ChannelID: "UCpekora"}},
		}))
		require.NoError(t, err)
		assert.False(t, policy.Enabled())
		assert.Equal(t, 1, policy.TargetChannelCount())
		assert.False(t, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeCommunity,
			ChannelID:   "UCpekora",
			PublishedAt: time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC),
		}))
	})

	t.Run("returns disabled policy when big-bang flag is off", func(t *testing.T) {
		t.Parallel()

		policy, err := buildCommunityShortsBigBangPolicy(config.IngestionConfig{}, nil)
		require.NoError(t, err)
		assert.False(t, policy.Enabled())
		assert.Zero(t, policy.TargetChannelCount())
	})

	t.Run("reuses operational target validation for duplicate channels", func(t *testing.T) {
		t.Parallel()

		_, err := buildCommunityShortsBigBangPolicy(config.IngestionConfig{
			CommunityShortsBigBangEnabled:   true,
			CommunityShortsBigBangCutoverAt: time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC),
		}, mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
			members: []*domain.Member{
				{Name: "Pekora", Org: "Hololive", ChannelID: "UCdup"},
				{Name: "Miko", Org: "Hololive", ChannelID: "UCdup"},
			},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate deployment targets")
	})
}

func TestBuildCommunityShortsRouteDecider(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	policy, err := buildCommunityShortsBigBangPolicy(config.IngestionConfig{
		CommunityShortsBigBangEnabled:   true,
		CommunityShortsBigBangCutoverAt: cutoverAt,
	}, mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
		members: []*domain.Member{{Name: "Pekora", Org: "Hololive", ChannelID: "UCpekora"}},
	}))
	require.NoError(t, err)

	decider := buildCommunityShortsRouteDecider(policy)
	require.NotNil(t, decider)
	assert.True(t, decider(poller.NotificationRouteRequest{
		AlarmType:   domain.AlarmTypeCommunity,
		ChannelID:   "UCpekora",
		PublishedAt: cutoverAt,
	}))
	assert.False(t, decider(poller.NotificationRouteRequest{
		AlarmType:   domain.AlarmTypeShorts,
		ChannelID:   "UCpekora",
		PublishedAt: cutoverAt.Add(-time.Second),
	}))
	assert.False(t, decider(poller.NotificationRouteRequest{
		AlarmType:   domain.AlarmTypeLive,
		ChannelID:   "UCpekora",
		PublishedAt: cutoverAt,
	}))
	assert.Nil(t, buildCommunityShortsRouteDecider(communityShortsBigBangPolicy{}))
}

func assertCommunityShortsPolicyTargetsMatchEnabledChannels(
	t *testing.T,
	policy communityShortsBigBangPolicy,
	channels []communityShortsOperationalChannel,
	publishedAt time.Time,
) {
	t.Helper()

	wantChannelIDs := communityShortsEnabledChannelIDs(channels)
	assert.ElementsMatch(t, wantChannelIDs, communityShortsPolicyTargetChannelIDs(policy))
	assert.Equal(t, len(wantChannelIDs), policy.TargetChannelCount())

	for i := range channels {
		channelID := channels[i].channelID
		if channelID == "" {
			continue
		}
		assert.Equal(t, channels[i].enabled, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeCommunity,
			ChannelID:   channelID,
			PublishedAt: publishedAt,
		}))
		assert.Equal(t, channels[i].enabled, policy.ShouldUseNewPath(communityShortsRouteRequest{
			AlarmType:   domain.AlarmTypeShorts,
			ChannelID:   channelID,
			PublishedAt: publishedAt,
		}))
	}
}

func communityShortsPolicyTargetChannelIDs(policy communityShortsBigBangPolicy) []string {
	channelIDs := make([]string, 0, len(policy.targetChannelIDs))
	for channelID := range policy.targetChannelIDs {
		channelIDs = append(channelIDs, channelID)
	}
	slices.Sort(channelIDs)
	return channelIDs
}
