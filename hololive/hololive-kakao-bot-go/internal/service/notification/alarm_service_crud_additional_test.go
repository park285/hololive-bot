package notification

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNewAlarmServiceAndCloseAllAlarmServices(t *testing.T) {
	ctx := context.Background()
	cacheSvc := newTestCacheService(t, ctx)

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
	ctx := context.Background()
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

	removed, err = as.RemoveAlarm(ctx, "room-1", "ch-1", nil)
	require.NoError(t, err)
	assert.False(t, removed)

	roomAlarms, err = as.GetRoomAlarms(ctx, "room-1")
	require.NoError(t, err)
	assert.Empty(t, roomAlarms)
}

func TestAlarmService_ClearRoomAlarms(t *testing.T) {
	ctx := context.Background()
	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: "ch-1", Name: "A"},
			{ChannelID: "ch-2", Name: "B"},
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

func TestAlarmService_SubmitPersistTaskAndWarmCache_NoRepository(t *testing.T) {
	called := false
	as := &AlarmService{logger: newDiscardAlarmLogger()}

	as.submitPersistTask("persist_alarm", func() {
		called = true
	})
	assert.False(t, called)

	require.NoError(t, as.WarmCacheFromDB(context.Background()))
}
