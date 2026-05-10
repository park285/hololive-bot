// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package notification

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

func seedAlarmChannelRegistry(t *testing.T, as *AlarmService, channelIDs ...string) {
	t.Helper()

	_, err := as.cache.SAdd(t.Context(), AlarmChannelRegistryKey, channelIDs)
	require.NoError(t, err)
}

func assertChzzkMapContains(t *testing.T, as *AlarmService, want map[string]string) {
	t.Helper()

	chzzkMap, err := as.cache.HGetAll(t.Context(), ChzzkChannelMapKey)
	require.NoError(t, err)
	require.Equal(t, want, chzzkMap)
}

func assertTwitchMaps(t *testing.T, as *AlarmService, wantLoginMap, wantChannelMap map[string]string) {
	t.Helper()

	twitchMap, err := as.cache.HGetAll(t.Context(), TwitchLoginMapKey)
	require.NoError(t, err)
	require.Equal(t, wantLoginMap, twitchMap)

	twitchChannelMap, err := as.cache.HGetAll(t.Context(), TwitchChannelLoginMapKey)
	require.NoError(t, err)
	require.Equal(t, wantChannelMap, twitchChannelMap)
}

func TestSyncPlatformMappings_WritesChzzkAndTwitchHashes(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{
				ChannelID:      "UC_alpha",
				ChzzkChannelID: "chzzk_alpha",
				TwitchUserID:   "AlphaLogin",
			},
			{
				ChannelID:      "UC_beta",
				ChzzkChannelID: "chzzk_beta",
			},
		},
	}

	seedAlarmChannelRegistry(t, as, "UC_alpha", "UC_beta", "UC_missing")
	require.NoError(t, as.SyncPlatformMappings(t.Context()))
	assertChzzkMapContains(t, as, map[string]string{"UC_alpha": "chzzk_alpha", "UC_beta": "chzzk_beta"})
	assertTwitchMaps(t, as, map[string]string{"alphalogin": "UC_alpha"}, map[string]string{"UC_alpha": "alphalogin"})
}

func TestSyncPlatformMappings_ClearsStaleHashes(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	require.NoError(t, as.cache.HSet(t.Context(), ChzzkChannelMapKey, "UC_stale", "chzzk_stale"))
	require.NoError(t, as.cache.HSet(t.Context(), TwitchLoginMapKey, "stale_login", "UC_stale"))
	require.NoError(t, as.SyncPlatformMappings(t.Context()))
	assertChzzkMapContains(t, as, map[string]string{})
	assertTwitchMaps(t, as, map[string]string{}, map[string]string{})

	chzzkEmpty, err := as.cache.Exists(t.Context(), ChzzkChannelMapEmptyKey)
	require.NoError(t, err)
	require.True(t, chzzkEmpty)

	twitchEmpty, err := as.cache.Exists(t.Context(), TwitchLoginMapEmptyKey)
	require.NoError(t, err)
	require.True(t, twitchEmpty)

	twitchChannelEmpty, err := as.cache.Exists(t.Context(), TwitchChannelLoginMapEmptyKey)
	require.NoError(t, err)
	require.True(t, twitchChannelEmpty)
}

func TestSyncPlatformMappingForChannel_AddAndRemoveIncrementally(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{
				ChannelID:      "UC_alpha",
				ChzzkChannelID: "chzzk_alpha",
				TwitchUserID:   "AlphaLogin",
			},
		},
	}

	seedAlarmChannelRegistry(t, as, "UC_alpha")
	require.NoError(t, as.cache.Set(t.Context(), ChzzkChannelMapEmptyKey, "1", 0))
	require.NoError(t, as.cache.Set(t.Context(), TwitchLoginMapEmptyKey, "1", 0))
	require.NoError(t, as.cache.Set(t.Context(), TwitchChannelLoginMapEmptyKey, "1", 0))
	require.NoError(t, as.syncPlatformMappingForChannel(t.Context(), "UC_alpha"))
	assertChzzkMapContains(t, as, map[string]string{"UC_alpha": "chzzk_alpha"})
	assertTwitchMaps(t, as, map[string]string{"alphalogin": "UC_alpha"}, map[string]string{"UC_alpha": "alphalogin"})

	chzzkEmpty, err := as.cache.Exists(t.Context(), ChzzkChannelMapEmptyKey)
	require.NoError(t, err)
	require.False(t, chzzkEmpty)

	twitchEmpty, err := as.cache.Exists(t.Context(), TwitchLoginMapEmptyKey)
	require.NoError(t, err)
	require.False(t, twitchEmpty)

	twitchChannelEmpty, err := as.cache.Exists(t.Context(), TwitchChannelLoginMapEmptyKey)
	require.NoError(t, err)
	require.False(t, twitchChannelEmpty)

	_, removeErr := as.cache.SRem(t.Context(), AlarmChannelRegistryKey, []string{"UC_alpha"})
	require.NoError(t, removeErr)
	require.NoError(t, as.syncPlatformMappingForChannel(t.Context(), "UC_alpha"))
	assertChzzkMapContains(t, as, map[string]string{})
	assertTwitchMaps(t, as, map[string]string{}, map[string]string{})
}

func TestSyncPlatformMappingForChannel_ReplacesTwitchLoginInO1Path(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{
				ChannelID:    "UC_alpha",
				TwitchUserID: "NewLogin",
			},
		},
	}

	seedAlarmChannelRegistry(t, as, "UC_alpha")
	require.NoError(t, as.cache.HSet(t.Context(), TwitchLoginMapKey, "oldlogin", "UC_alpha"))
	require.NoError(t, as.cache.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_alpha", "oldlogin"))
	require.NoError(t, as.syncPlatformMappingForChannel(t.Context(), "UC_alpha"))
	assertTwitchMaps(t, as, map[string]string{"newlogin": "UC_alpha"}, map[string]string{"UC_alpha": "newlogin"})
}
