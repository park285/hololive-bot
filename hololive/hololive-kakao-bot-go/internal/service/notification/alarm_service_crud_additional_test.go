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
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAlarmServiceAndCloseAllAlarmServices(t *testing.T) {
	ctx := t.Context()
	cacheSvc := newTestCacheService(ctx, t)

	svc, err := NewAlarmService(
		cacheSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		newDiscardAlarmLogger(),
		[]int{10, 3, 1, 3},
	)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.Equal(t, []int{10, 3, 1}, svc.GetTargetMinutes())

	require.NoError(t, CloseAllAlarmServices(ctx))
	require.NoError(t, svc.Close(ctx))
}

func TestAlarmService_AddRemoveAndGetRoomAlarms(t *testing.T) {
	ctx := t.Context()
	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{
				ChannelID:      "ch-1",
				Name:           "Miko",
				ChzzkChannelID: "chzzk-1",
				TwitchUserID:   "miko_live",
			},
		},
	}

	added, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     "room-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		MemberName: "Miko",
		RoomName:   "메인방",
		UserName:   "관리자",
	})
	require.NoError(t, err)
	assert.True(t, added)
	assertPlatformMappings(ctx, t, as, "ch-1", "chzzk-1", "miko_live")

	// 중복 등록은 false여야 한다.
	added, err = as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     "room-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		MemberName: "Miko",
	})
	require.NoError(t, err)
	assert.False(t, added)

	roomAlarms, err := as.GetRoomAlarms(ctx, "room-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"ch-1"}, roomAlarms)

	roomName, err := as.cache.HGet(ctx, RoomNamesCacheKey, "room-1")
	require.NoError(t, err)
	assert.Equal(t, "메인방", roomName)

	userName, err := as.cache.HGet(ctx, UserNamesCacheKey, "user-1")
	require.NoError(t, err)
	assert.Equal(t, "관리자", userName)

	// repo가 없는 상태에서 타입 포함 조회는 오류여야 한다.
	_, err = as.GetRoomAlarmsWithTypes(ctx, "room-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alarm repository not configured")

	removed, err := as.RemoveAlarm(ctx, "room-1", "ch-1", nil)
	require.NoError(t, err)
	assert.True(t, removed)
	assertPlatformMappings(ctx, t, as, "ch-1", "", "")

	removed, err = as.RemoveAlarm(ctx, "room-1", "ch-1", nil)
	require.NoError(t, err)
	assert.False(t, removed)

	roomAlarms, err = as.GetRoomAlarms(ctx, "room-1")
	require.NoError(t, err)
	assert.Empty(t, roomAlarms)
}

func TestAlarmService_ClearRoomAlarms(t *testing.T) {
	ctx := t.Context()
	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: "ch-1", Name: "A", ChzzkChannelID: "chzzk-1", TwitchUserID: "a_live"},
			{ChannelID: "ch-2", Name: "B", ChzzkChannelID: "chzzk-2", TwitchUserID: "b_live"},
		},
	}

	// 빈 방은 no-op
	count, err := as.ClearRoomAlarms(ctx, "room-1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	_, err = as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     "room-1",
		UserID:     "user-1",
		ChannelID:  "ch-1",
		MemberName: "A",
	})
	require.NoError(t, err)

	_, err = as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     "room-1",
		UserID:     "user-1",
		ChannelID:  "ch-2",
		MemberName: "B",
	})
	require.NoError(t, err)

	count, err = as.ClearRoomAlarms(ctx, "room-1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assertPlatformMappings(ctx, t, as, "ch-1", "", "")
	assertPlatformMappings(ctx, t, as, "ch-2", "", "")

	alarms, err := as.GetRoomAlarms(ctx, "room-1")
	require.NoError(t, err)
	assert.Empty(t, alarms)

	registryRooms, err := as.cache.SMembers(ctx, AlarmRegistryKey)
	require.NoError(t, err)
	assert.NotContains(t, registryRooms, "room-1")

	channelRegistry, err := as.cache.SMembers(ctx, AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.Empty(t, channelRegistry)
}

func assertPlatformMappings(ctx context.Context, t *testing.T, as *AlarmService, channelID, wantChzzk, wantTwitchLogin string) {
	t.Helper()

	chzzkMap, err := as.cache.HGetAll(ctx, ChzzkChannelMapKey)
	require.NoError(t, err)
	assert.Equal(t, wantChzzk, chzzkMap[channelID])

	twitchMap, err := as.cache.HGetAll(ctx, TwitchLoginMapKey)
	require.NoError(t, err)

	if wantTwitchLogin == "" {
		for _, mappedChannelID := range twitchMap {
			assert.NotEqual(t, channelID, mappedChannelID)
		}

		return
	}

	assert.Equal(t, channelID, twitchMap[wantTwitchLogin])
}

func TestWarmCacheFromDB_UsesAuthoritativeRebuildPath(t *testing.T) {
	as := newTestAlarmService(t)

	as.alarmRepo = &sharedalarm.Repository{}

	original := rebuildSubscriberCacheFromRepository
	rebuildCalled := false

	rebuildSubscriberCacheFromRepository = func(_ context.Context, cacheSvc cache.Client, repo *sharedalarm.Repository) (sharedalarm.CacheWarmSummary, error) {
		rebuildCalled = true

		assert.Same(t, as.cache, cacheSvc)
		assert.Same(t, as.alarmRepo, repo)

		return sharedalarm.CacheWarmSummary{AlarmCount: 1, RoomCount: 1, ChannelCount: 1}, nil
	}

	t.Cleanup(func() {
		rebuildSubscriberCacheFromRepository = original
	})

	require.NoError(t, as.WarmCacheFromDB(t.Context()))
	assert.True(t, rebuildCalled)
}

func TestWarmCacheFromDB_RebuildFailureRecordsMetric(t *testing.T) {
	as := newTestAlarmService(t)

	as.alarmRepo = &sharedalarm.Repository{}

	original := rebuildSubscriberCacheFromRepository

	rebuildSubscriberCacheFromRepository = func(_ context.Context, _ cache.Client, _ *sharedalarm.Repository) (sharedalarm.CacheWarmSummary, error) {
		return sharedalarm.CacheWarmSummary{}, errors.New("repo load failed")
	}

	t.Cleanup(func() {
		rebuildSubscriberCacheFromRepository = original
	})

	before := counterValueForLabels(t, alarmCacheRebuildMetricName, map[string]string{
		"operation": "warm",
		"result":    "error",
	})

	err := as.WarmCacheFromDB(t.Context())
	require.Error(t, err)
	assert.InDelta(t, before+1, counterValueForLabels(t, alarmCacheRebuildMetricName, map[string]string{
		"operation": "warm",
		"result":    "error",
	}), 0.000001)
}

func TestWarmCacheFromDB_SuccessRecordsDurationAndSummaryMetrics(t *testing.T) {
	as := newTestAlarmService(t)

	as.alarmRepo = &sharedalarm.Repository{}

	original := rebuildSubscriberCacheFromRepository

	rebuildSubscriberCacheFromRepository = func(_ context.Context, _ cache.Client, _ *sharedalarm.Repository) (sharedalarm.CacheWarmSummary, error) {
		return sharedalarm.CacheWarmSummary{
			AlarmCount:   5,
			RoomCount:    3,
			ChannelCount: 2,
		}, nil
	}

	t.Cleanup(func() {
		rebuildSubscriberCacheFromRepository = original
	})

	beforeDurationCount := histogramCountForLabels(t, alarmCacheRebuildDurationMetricName, map[string]string{
		"operation": "warm",
		"result":    "ok",
	})

	require.NoError(t, as.WarmCacheFromDB(t.Context()))
	assert.Equal(t, beforeDurationCount+1, histogramCountForLabels(t, alarmCacheRebuildDurationMetricName, map[string]string{
		"operation": "warm",
		"result":    "ok",
	}))
	assert.InDelta(t, 5.0, gaugeValueForLabels(t, alarmCacheRebuildLoadedMetricName, map[string]string{
		"operation": "warm",
		"resource":  "alarms",
	}), 0.000001)
	assert.InDelta(t, 3.0, gaugeValueForLabels(t, alarmCacheRebuildLoadedMetricName, map[string]string{
		"operation": "warm",
		"resource":  "rooms",
	}), 0.000001)
	assert.InDelta(t, 2.0, gaugeValueForLabels(t, alarmCacheRebuildLoadedMetricName, map[string]string{
		"operation": "warm",
		"resource":  "channels",
	}), 0.000001)
}
