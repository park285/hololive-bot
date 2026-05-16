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

package alarmservice

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddAlarm_CacheWrite(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	req := domain.AddAlarmRequest{
		RoomID:     "room1",
		UserID:     "user1",
		ChannelID:  "UC_TEST",
		MemberName: "테스트 멤버",
		RoomName:   "테스트 방",
		UserName:   "테스트 사용자",
	}

	added, err := as.AddAlarm(ctx, req)
	require.NoError(t, err)
	assert.True(t, added)

	channels, err := as.cache.SMembers(ctx, AlarmKeyPrefix+"room1")
	require.NoError(t, err)
	assert.Contains(t, channels, "UC_TEST")

	registry, err := as.cache.SMembers(ctx, AlarmRegistryKey)
	require.NoError(t, err)
	assert.Contains(t, registry, "room1")

	channelReg, err := as.cache.SMembers(ctx, AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.Contains(t, channelReg, "UC_TEST")

	name, err := as.cache.HGet(ctx, MemberNameKey, "UC_TEST")
	require.NoError(t, err)
	assert.Equal(t, "테스트 멤버", name)
}

func TestAddAlarm_ClearsEmptySubscriberCacheMarker(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()
	require.NoError(t, as.cache.Set(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey, "1", 0))

	added, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:    "room1",
		UserID:    "user1",
		ChannelID: "UC_FIRST",
	})
	require.NoError(t, err)
	require.True(t, added)

	emptyMarkerExists, err := as.cache.Exists(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey)
	require.NoError(t, err)
	assert.False(t, emptyMarkerExists)
}

func TestAddAlarm_DuplicateReturnsNotAdded(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	req := domain.AddAlarmRequest{
		RoomID:     "room1",
		ChannelID:  "UC_TEST",
		MemberName: "멤버",
	}

	added1, err := as.AddAlarm(ctx, req)
	require.NoError(t, err)
	assert.True(t, added1)

	added2, err := as.AddAlarm(ctx, req)
	require.NoError(t, err)
	assert.False(t, added2)
}

func TestRemoveAlarm_Success(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	req := domain.AddAlarmRequest{
		RoomID:     "room1",
		ChannelID:  "UC_TEST",
		MemberName: "멤버",
	}
	_, err := as.AddAlarm(ctx, req)
	require.NoError(t, err)

	removed, err := as.RemoveAlarm(ctx, "room1", "UC_TEST", nil)
	require.NoError(t, err)
	assert.True(t, removed)

	channels, err := as.cache.SMembers(ctx, AlarmKeyPrefix+"room1")
	require.NoError(t, err)
	assert.Empty(t, channels)
}

func TestRemoveAlarm_NotFound(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	removed, err := as.RemoveAlarm(ctx, "room1", "UC_NONEXIST", nil)
	require.NoError(t, err)
	assert.False(t, removed)
}

func TestGetRoomAlarms_WithAlarms(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	for _, ch := range []string{"UC_A", "UC_B", "UC_C"} {
		_, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
			RoomID:    "room1",
			ChannelID: ch,
		})
		require.NoError(t, err)
	}

	channels, err := as.GetRoomAlarms(ctx, "room1")
	require.NoError(t, err)
	assert.Len(t, channels, 3)
}

func TestGetRoomAlarms_EmptyRoom(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	channels, err := as.GetRoomAlarms(ctx, "room_empty")
	require.NoError(t, err)
	assert.Empty(t, channels)
}

func TestClearRoomAlarms_ClearsAll(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	for _, ch := range []string{"UC_A", "UC_B"} {
		_, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
			RoomID:    "room1",
			ChannelID: ch,
		})
		require.NoError(t, err)
	}

	cleared, err := as.ClearRoomAlarms(ctx, "room1")
	require.NoError(t, err)
	assert.Equal(t, 2, cleared)

	channels, err := as.GetRoomAlarms(ctx, "room1")
	require.NoError(t, err)
	assert.Empty(t, channels)

	registry, err := as.cache.SMembers(ctx, AlarmRegistryKey)
	require.NoError(t, err)
	assert.NotContains(t, registry, "room1")
}

func TestClearRoomAlarms_EmptyRoom(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	cleared, err := as.ClearRoomAlarms(ctx, "room_empty")
	require.NoError(t, err)
	assert.Equal(t, 0, cleared)
}

func TestMarkAsNotified_SetsFlag(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	start := time.Now().UTC().Truncate(time.Minute)
	err := as.MarkAsNotified(ctx, "stream1", start, 5)
	require.NoError(t, err)

	var data NotifiedData

	err = as.cache.Get(ctx, NotifiedKeyPrefix+"stream1", &data)
	require.NoError(t, err)
	assert.True(t, data.SentAt[5])
	assert.Equal(t, start.Format(time.RFC3339), data.StartScheduled)
}

func TestMarkAsNotified_ScheduleChangeResetsMap(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	start1 := time.Date(2026, time.March, 2, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, time.March, 2, 11, 0, 0, 0, time.UTC)

	err := as.MarkAsNotified(ctx, "stream1", start1, 5)
	require.NoError(t, err)

	err = as.MarkAsNotified(ctx, "stream1", start2, 3)
	require.NoError(t, err)

	var data NotifiedData

	err = as.cache.Get(ctx, NotifiedKeyPrefix+"stream1", &data)
	require.NoError(t, err)

	assert.False(t, data.SentAt[5])
	assert.True(t, data.SentAt[3])
	assert.Equal(t, start2.Format(time.RFC3339), data.StartScheduled)
}

func TestGetTargetMinutes_Default(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	got := as.GetTargetMinutes()
	assert.Equal(t, []int{30, 15, 5, 1}, got) // newTestAlarmService에서 설정
}

func TestUpdateAlarmAdvanceMinutes(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	result := as.UpdateAlarmAdvanceMinutes(t.Context(), 20)

	assert.Contains(t, result, 20)
	assert.Contains(t, result, 1)

	got := as.GetTargetMinutes()
	assert.Equal(t, result, got)
}

func TestCacheMemberName_RoundTrip(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	err := as.CacheMemberName(ctx, "UC_TEST", "페코라")
	require.NoError(t, err)

	name, err := as.GetMemberName(ctx, "UC_TEST")
	require.NoError(t, err)
	assert.Equal(t, "페코라", name)
}

func TestSetRoomName(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	err := as.SetRoomName(ctx, "room1", "테스트 방")
	require.NoError(t, err)

	name, err := as.cache.HGet(ctx, RoomNamesCacheKey, "room1")
	require.NoError(t, err)
	assert.Equal(t, "테스트 방", name)
}

func TestSetUserName(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	err := as.SetUserName(ctx, "user1", "테스트 사용자")
	require.NoError(t, err)

	name, err := as.cache.HGet(ctx, UserNamesCacheKey, "user1")
	require.NoError(t, err)
	assert.Equal(t, "테스트 사용자", name)
}

func TestGetAllAlarmKeys(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	_, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     "room1",
		ChannelID:  "UC_A",
		MemberName: "멤버A",
	})
	require.NoError(t, err)

	entries, err := as.GetAllAlarmKeys(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 1)
}

func TestAddAlarmClearsSubscriberCacheEmptyMarkerAndBumpsChannelRegistryVersion(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()
	require.NoError(t, as.cache.Set(ctx, AlarmSubscriberCacheEmptyKey, "1", 0))

	added, err := as.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:    "room-1",
		UserID:    "user-1",
		ChannelID: "UC_TEST",
		AlarmTypes: domain.AlarmTypes{
			domain.AlarmTypeLive,
		},
	})
	require.NoError(t, err)
	require.True(t, added)

	emptyMarkerExists, err := as.cache.Exists(ctx, AlarmSubscriberCacheEmptyKey)
	require.NoError(t, err)
	assert.False(t, emptyMarkerExists)

	var version int64
	require.NoError(t, as.cache.Get(ctx, AlarmChannelRegistryVersionKey, &version))
	assert.Positive(t, version)
}

func TestGetDistinctRooms(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	_, _ = as.AddAlarm(ctx, domain.AddAlarmRequest{RoomID: "room1", ChannelID: "UC_A"})
	_, _ = as.AddAlarm(ctx, domain.AddAlarmRequest{RoomID: "room2", ChannelID: "UC_B"})

	rooms, err := as.GetDistinctRooms(ctx)
	require.NoError(t, err)
	assert.Len(t, rooms, 2)
}

func TestAlarmServiceClose(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cacheSvc := newTestCacheService(ctx, t)

	svc, err := NewAlarmService(cacheSvc, nil, nil, nil, nil, nil, nil, []int{5, 3, 1})
	require.NoError(t, err)

	err = svc.Close(ctx)
	require.NoError(t, err)

	err = svc.Close(ctx)
	require.NoError(t, err)
}

func TestWarmCacheFromDB_NilRepo(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	err := as.WarmCacheFromDB(ctx)
	assert.NoError(t, err)
}
