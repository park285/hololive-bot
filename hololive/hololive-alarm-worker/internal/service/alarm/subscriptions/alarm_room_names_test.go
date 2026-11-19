package subscriptions

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const memberSubscriptionNeonID = "yoinagi-neon"

func roomNamesByRoom(t *testing.T, as *AlarmService) map[string]string {
	t.Helper()

	entries, err := as.GetAllAlarmKeys(t.Context())
	require.NoError(t, err)

	names := make(map[string]string, len(entries))
	for _, entry := range entries {
		names[entry.RoomID] = entry.RoomName
	}

	return names
}

func TestGetAllAlarmKeysListsDistinctRoomChannelsFromRepository(t *testing.T) {
	ctx := t.Context()
	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{
		{ChannelID: testAlphaChannelID, Name: "A", ShortKoreanName: "에이"},
		{ChannelID: memberSubscriptionChannel, Name: "유닛 B"},
	}}

	for _, req := range []*domain.AddAlarmRequest{
		{RoomID: testRoomID, UserID: "user-1", ChannelID: testAlphaChannelID, RoomName: "카카오방"},
		{RoomID: testRoomID, UserID: "user-1", ChannelID: memberSubscriptionChannel, HostID: memberSubscriptionMiraID},
		{RoomID: testRoomID, UserID: "user-1", ChannelID: memberSubscriptionChannel, HostID: memberSubscriptionNeonID},
		{RoomID: "room-2", UserID: "user-2", ChannelID: testAlphaChannelID},
	} {
		added, err := as.AddAlarm(ctx, req)
		require.NoError(t, err)
		require.True(t, added)
	}

	err := as.cache.ScanKeyPages(ctx, "*", 100, func(keys []string) error {
		if _, err := as.cache.DelMany(ctx, keys); err != nil {
			return fmt.Errorf("clear subscriber cache page: %w", err)
		}

		return nil
	})
	require.NoError(t, err)

	entries, err := as.GetAllAlarmKeys(ctx)
	require.NoError(t, err)

	count, err := as.CountAlarmEntries(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, count, "host 구독 두 행을 같은 방·채널 한 쌍으로 집계해야 한다")

	got := make([]domain.AlarmEntry, 0, len(entries))
	for _, entry := range entries {
		got = append(got, domain.AlarmEntry{RoomID: entry.RoomID, RoomName: entry.RoomName, ChannelID: entry.ChannelID})
	}

	assert.ElementsMatch(t, []domain.AlarmEntry{
		{RoomID: testRoomID, RoomName: "카카오방", ChannelID: testAlphaChannelID},
		{RoomID: testRoomID, RoomName: "카카오방", ChannelID: memberSubscriptionChannel},
		{RoomID: "room-2", RoomName: "room-2", ChannelID: testAlphaChannelID},
	}, got)
}

func TestSetRoomNamePrefersAdminNameAcrossRebuildAndReregistrationUntilCleared(t *testing.T) {
	ctx := t.Context()
	as := newTestAlarmService(t)

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	_, err := as.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testRoomID, UserID: testUserID, ChannelID: testChannelID, RoomName: "카카오 이름"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{testRoomID: "카카오 이름"}, roomNamesByRoom(t, as))

	require.NoError(t, as.SetRoomName(ctx, testRoomID, "  관리 이름  "))
	assert.Equal(t, map[string]string{testRoomID: "관리 이름"}, roomNamesByRoom(t, as))

	require.NoError(t, as.WarmCacheFromDB(ctx))
	assert.Equal(t, map[string]string{testRoomID: "관리 이름"}, roomNamesByRoom(t, as))

	cleared, err := as.ClearRoomAlarms(ctx, testRoomID)
	require.NoError(t, err)
	require.Equal(t, 1, cleared)
	assert.Empty(t, roomNamesByRoom(t, as))

	_, err = as.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testRoomID, UserID: testUserID, ChannelID: testOtherChannelID, RoomName: "새 카카오 이름"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{testRoomID: "관리 이름"}, roomNamesByRoom(t, as))

	require.NoError(t, as.SetRoomName(ctx, testRoomID, " "))
	assert.Equal(t, map[string]string{testRoomID: "새 카카오 이름"}, roomNamesByRoom(t, as))
	require.NoError(t, as.SetRoomName(ctx, testRoomID, ""))
}

func TestSetRoomNameRejectsBlankRoomID(t *testing.T) {
	as := newTestAlarmService(t)

	require.ErrorContains(t, as.SetRoomName(t.Context(), " ", "관리 이름"), "room id is required")
}
