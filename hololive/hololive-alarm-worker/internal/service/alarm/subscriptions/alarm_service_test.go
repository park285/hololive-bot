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

package subscriptions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

// members에 표시명이 없으면 요청의 member_name을 대신 쓰지 않고 빈 표시명을 캐시한다. 알림 표시 단계가 종단 문구를 쓴다.
func TestAddAlarm_CacheWrite(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	req := domain.AddAlarmRequest{
		RoomID:     testAltRoomID,
		UserID:     "user1",
		ChannelID:  testUCChannelID,
		MemberName: "테스트 멤버",
		RoomName:   "테스트 방",
		UserName:   "테스트 사용자",
	}

	added, err := as.AddAlarm(ctx, &req)
	require.NoError(t, err)
	assert.True(t, added)

	subscribers, err := as.GetChannelSubscribersByType(ctx, testUCChannelID, domain.AlarmTypeLive)
	require.NoError(t, err)
	assert.Equal(t, []string{testAltRoomID}, subscribers)

	channelReg, err := as.cache.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.Contains(t, channelReg, testUCChannelID)

	name, err := as.cache.HGet(ctx, sharedalarmkeys.MemberNameKey, testUCChannelID)
	require.NoError(t, err)
	assert.Empty(t, name)
}

func TestAddAlarm_CacheWriteUsesShortKoreanMemberName(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{{
		ChannelID:       testUCChannelID,
		Name:            "Juufuutei Raden",
		NameKo:          "주후테이 라덴",
		ShortKoreanName: "라덴",
	}}}

	added, err := as.AddAlarm(t.Context(), &domain.AddAlarmRequest{
		RoomID:     testAltRoomID,
		UserID:     "user1",
		ChannelID:  testUCChannelID,
		MemberName: "Juufuutei Raden",
		RoomName:   "테스트 방",
		UserName:   "테스트 사용자",
	})
	require.NoError(t, err)
	require.True(t, added)

	name, err := as.cache.HGet(t.Context(), sharedalarmkeys.MemberNameKey, testUCChannelID)
	require.NoError(t, err)
	assert.Equal(t, "라덴", name)
}

func TestAddAlarm_ClearsEmptySubscriberCacheMarker(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()
	require.NoError(t, as.cache.Set(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey, "1", 0))

	added, err := as.AddAlarm(ctx, &domain.AddAlarmRequest{
		RoomID:    testAltRoomID,
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
		RoomID:     testAltRoomID,
		ChannelID:  testUCChannelID,
		MemberName: "멤버",
	}

	added1, err := as.AddAlarm(ctx, &req)
	require.NoError(t, err)
	assert.True(t, added1)

	added2, err := as.AddAlarm(ctx, &req)
	require.NoError(t, err)
	assert.False(t, added2)
}

func TestRemoveAlarm_Success(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	req := domain.AddAlarmRequest{
		RoomID:     testAltRoomID,
		ChannelID:  testUCChannelID,
		MemberName: "멤버",
	}
	_, err := as.AddAlarm(ctx, &req)
	require.NoError(t, err)

	removed, err := as.RemoveAlarm(ctx, testAltRoomID, testUCChannelID, nil)
	require.NoError(t, err)
	assert.True(t, removed)

	channels, err := as.GetRoomAlarms(ctx, testAltRoomID)
	require.NoError(t, err)
	assert.Empty(t, channels)

	subscribers, err := as.GetChannelSubscribersByType(ctx, testUCChannelID, domain.AlarmTypeLive)
	require.NoError(t, err)
	assert.Empty(t, subscribers)
}

func TestRemoveAlarm_NotFound(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	removed, err := as.RemoveAlarm(ctx, testAltRoomID, "UC_NONEXIST", nil)
	require.NoError(t, err)
	assert.False(t, removed)
}

func TestGetRoomAlarms_WithAlarms(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	for _, ch := range []string{"UC_A", "UC_B", "UC_C"} {
		_, err := as.AddAlarm(ctx, &domain.AddAlarmRequest{
			RoomID:    testAltRoomID,
			ChannelID: ch,
		})
		require.NoError(t, err)
	}

	channels, err := as.GetRoomAlarms(ctx, testAltRoomID)
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
		_, err := as.AddAlarm(ctx, &domain.AddAlarmRequest{
			RoomID:    testAltRoomID,
			ChannelID: ch,
		})
		require.NoError(t, err)
	}

	cleared, err := as.ClearRoomAlarms(ctx, testAltRoomID)
	require.NoError(t, err)
	assert.Equal(t, 2, cleared)

	channels, err := as.GetRoomAlarms(ctx, testAltRoomID)
	require.NoError(t, err)
	assert.Empty(t, channels)

	subscribers, err := as.GetChannelSubscribersByType(ctx, "UC_A", domain.AlarmTypeLive)
	require.NoError(t, err)
	assert.Empty(t, subscribers)
}

func TestClearRoomAlarms_EmptyRoom(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	cleared, err := as.ClearRoomAlarms(ctx, "room_empty")
	require.NoError(t, err)
	assert.Equal(t, 0, cleared)
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
	result, err := as.UpdateAlarmAdvanceMinutes(t.Context(), 20)
	require.NoError(t, err)
	assert.Equal(t, domain.ApplyConfirmed, result.Outcome)
	assert.Equal(t, 20, result.RequestedMinutes)

	assert.Contains(t, result.TargetMinutes, 20)
	assert.Contains(t, result.TargetMinutes, 1)

	got := as.GetTargetMinutes()
	assert.Equal(t, result.TargetMinutes, got)
}

func TestCacheMemberName_RoundTrip(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	err := as.CacheMemberName(ctx, testUCChannelID, "페코라")
	require.NoError(t, err)

	name, err := as.GetMemberName(ctx, testUCChannelID)
	require.NoError(t, err)
	assert.Equal(t, "페코라", name)
}

func TestGetAllAlarmKeys(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	ctx := t.Context()

	_, err := as.AddAlarm(ctx, &domain.AddAlarmRequest{
		RoomID:     testAltRoomID,
		ChannelID:  "UC_A",
		MemberName: "멤버A",
	})
	require.NoError(t, err)

	entries, err := as.GetAllAlarmKeys(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 1)
}
