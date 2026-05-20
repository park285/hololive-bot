package communityshorts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

func TestBuildPolicy(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	policy, err := BuildPolicy(config.IngestionConfig{
		CommunityShortsBigBangEnabled:   true,
		CommunityShortsBigBangCutoverAt: cutoverAt,
	}, BuildOperationalChannelsFromMembers([]*domain.Member{
		{Name: "Pekora", Org: "Hololive", ChannelID: "UCpekora"},
		{Name: "Miko", Org: "Hololive", ChannelID: "UCmiko"},
		{Name: "Graduated", Org: "Hololive", ChannelID: "UCgraduated", IsGraduated: true},
	}))
	require.NoError(t, err)
	assert.True(t, policy.Enabled())
	assert.Equal(t, cutoverAt, policy.CutoverAt())
	assert.Equal(t, []string{"UCmiko", "UCpekora"}, policy.TargetChannelIDs())
	assert.True(t, policy.ShouldUseNewPath(RouteRequest{AlarmType: domain.AlarmTypeCommunity, ChannelID: "UCpekora", PublishedAt: cutoverAt}))
	assert.True(t, policy.ShouldUseNewPath(RouteRequest{AlarmType: domain.AlarmTypeShorts, ChannelID: "UCmiko", PublishedAt: cutoverAt.Add(time.Second)}))
	assert.False(t, policy.ShouldUseNewPath(RouteRequest{AlarmType: domain.AlarmTypeCommunity, ChannelID: "UCpekora", PublishedAt: cutoverAt.Add(-time.Second)}))
	assert.False(t, policy.ShouldUseNewPath(RouteRequest{AlarmType: domain.AlarmTypeLive, ChannelID: "UCpekora", PublishedAt: cutoverAt}))
	assert.False(t, policy.ShouldUseNewPath(RouteRequest{AlarmType: domain.AlarmTypeShorts, ChannelID: "UCunknown", PublishedAt: cutoverAt}))
	assert.False(t, policy.ShouldUseNewPath(RouteRequest{AlarmType: domain.AlarmTypeShorts, ChannelID: "UCmiko"}))
}

func TestBuildPolicy_DisabledCases(t *testing.T) {
	t.Parallel()

	policy, err := BuildPolicy(config.IngestionConfig{}, nil)
	require.NoError(t, err)
	assert.False(t, policy.Enabled())
	assert.Zero(t, policy.TargetChannelCount())

	policy, err = BuildPolicy(config.IngestionConfig{CommunityShortsBigBangEnabled: true}, []OperationalChannel{{OwnerLabel: "Pekora", ChannelID: "UCpekora", Enabled: true}})
	require.NoError(t, err)
	assert.False(t, policy.Enabled())
	assert.Equal(t, 1, policy.TargetChannelCount())
}

func TestBuildRouteDecider(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	policy, err := BuildPolicy(config.IngestionConfig{
		CommunityShortsBigBangEnabled:   true,
		CommunityShortsBigBangCutoverAt: cutoverAt,
	}, []OperationalChannel{{OwnerLabel: "Pekora", ChannelID: "UCpekora", Enabled: true}})
	require.NoError(t, err)

	decider := BuildRouteDecider(policy)
	require.NotNil(t, decider)
	assert.True(t, decider(poller.NotificationRouteRequest{AlarmType: domain.AlarmTypeCommunity, ChannelID: "UCpekora", PublishedAt: cutoverAt}))
	assert.False(t, decider(poller.NotificationRouteRequest{AlarmType: domain.AlarmTypeShorts, ChannelID: "UCpekora", PublishedAt: cutoverAt.Add(-time.Second)}))
	assert.False(t, decider(poller.NotificationRouteRequest{AlarmType: domain.AlarmTypeLive, ChannelID: "UCpekora", PublishedAt: cutoverAt}))
	assert.Nil(t, BuildRouteDecider(Policy{}))
}
