package alarm

import (
	"context"
	"sort"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarmSubscriberCacheFromAlarms_WritesTypeSpecificSubscriptions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheSvc := newMemoryCacheClient()

	summary, err := WarmSubscriberCacheFromAlarms(ctx, cacheSvc, []*domain.Alarm{
		{
			RoomID:     "room-community",
			UserID:     "user-community",
			ChannelID:  "UC_A",
			MemberName: "Member A",
			RoomName:   "Community Room",
			UserName:   "Community User",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
		},
		{
			RoomID:     "room-shorts",
			UserID:     "user-shorts",
			ChannelID:  "UC_A",
			MemberName: "Member A",
			RoomName:   "Shorts Room",
			UserName:   "Shorts User",
			AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
		},
		{
			RoomID:     "room-default",
			UserID:     "user-default",
			ChannelID:  "UC_B",
			MemberName: "Member B",
			RoomName:   "Default Room",
			UserName:   "Default User",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, CacheWarmSummary{AlarmCount: 3, RoomCount: 3, ChannelCount: 2}, summary)

	roomChannels, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildRoomAlarmKey("room-community"))
	require.NoError(t, err)
	assert.Equal(t, []string{"UC_A"}, roomChannels)

	registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"room-community", "room-shorts", "room-default"}, registryRooms)

	channelRegistry, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"UC_A", "UC_B"}, channelRegistry)

	communitySubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_A", domain.AlarmTypeCommunity))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-community"}, communitySubs)

	shortsSubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_A", domain.AlarmTypeShorts))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-shorts"}, shortsSubs)

	liveSubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_A", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Empty(t, liveSubs)

	defaultLiveSubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_B", domain.AlarmTypeLive))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-default"}, defaultLiveSubs)

	defaultCommunitySubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_B", domain.AlarmTypeCommunity))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-default"}, defaultCommunitySubs)

	defaultShortsSubs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.BuildChannelSubscriberKey("UC_B", domain.AlarmTypeShorts))
	require.NoError(t, err)
	assert.Equal(t, []string{"room-default"}, defaultShortsSubs)

	memberName, err := cacheSvc.HGet(ctx, sharedalarmkeys.MemberNameKey, "UC_A")
	require.NoError(t, err)
	assert.Equal(t, "Member A", memberName)

	roomName, err := cacheSvc.HGet(ctx, sharedalarmkeys.RoomNamesCacheKey, "room-shorts")
	require.NoError(t, err)
	assert.Equal(t, "Shorts Room", roomName)

	userName, err := cacheSvc.HGet(ctx, sharedalarmkeys.UserNamesCacheKey, "user-default")
	require.NoError(t, err)
	assert.Equal(t, "Default User", userName)
}

func newMemoryCacheClient() *cachemocks.Client {
	sets := make(map[string]map[string]struct{})
	hashes := make(map[string]map[string]string)

	client := cachemocks.NewStrictClient()
	client.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		if sets[key] == nil {
			sets[key] = make(map[string]struct{})
		}
		var added int64
		for _, member := range members {
			if _, exists := sets[key][member]; exists {
				continue
			}
			sets[key][member] = struct{}{}
			added++
		}
		return added, nil
	}
	client.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		members := make([]string, 0, len(sets[key]))
		for member := range sets[key] {
			members = append(members, member)
		}
		sort.Strings(members)
		return members, nil
	}
	client.HSetFunc = func(_ context.Context, key, field, value string) error {
		if hashes[key] == nil {
			hashes[key] = make(map[string]string)
		}
		hashes[key][field] = value
		return nil
	}
	client.HGetFunc = func(_ context.Context, key, field string) (string, error) {
		if hashes[key] == nil {
			return "", nil
		}
		return hashes[key][field], nil
	}

	return client
}
