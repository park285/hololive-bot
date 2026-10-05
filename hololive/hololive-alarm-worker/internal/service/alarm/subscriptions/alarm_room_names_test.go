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

	// 관리 목록은 Valkey 방 index가 아니라 PG에서 만든다. subscriber cache를 비워도 목록은 그대로다.
	err := as.cache.ScanKeyPages(ctx, "*", 100, func(keys []string) error {
		if _, err := as.cache.DelMany(ctx, keys); err != nil {
			return fmt.Errorf("clear subscriber cache page: %w", err)
		}

		return nil
	})
	require.NoError(t, err)

	entries, err := as.GetAllAlarmKeys(ctx)
	require.NoError(t, err)

	got := make([]domain.AlarmEntry, 0, len(entries))
	for _, entry := range entries {
		got = append(got, domain.AlarmEntry{RoomID: entry.RoomID, RoomName: entry.RoomName, ChannelID: entry.ChannelID})
	}

	// 같은 채널의 UNIT B 멤버 구독 두 행은 (방, 채널) 한 항목이다. Kakao 방 이름이 없는 방은 방 ID로 표시한다.
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

	// 전체 해지 뒤 Kakao가 새 방 이름으로 재등록해도 관리자 이름이 이긴다.
	cleared, err := as.ClearRoomAlarms(ctx, testRoomID)
	require.NoError(t, err)
	require.Equal(t, 1, cleared)
	assert.Empty(t, roomNamesByRoom(t, as))

	_, err = as.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testRoomID, UserID: testUserID, ChannelID: testOtherChannelID, RoomName: "새 카카오 이름"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{testRoomID: "관리 이름"}, roomNamesByRoom(t, as))

	// 공백 이름은 관리자 지정을 해제해 Kakao 방 이름으로 돌아간다. 이미 해제된 방의 해제도 성공한다.
	require.NoError(t, as.SetRoomName(ctx, testRoomID, " "))
	assert.Equal(t, map[string]string{testRoomID: "새 카카오 이름"}, roomNamesByRoom(t, as))
	require.NoError(t, as.SetRoomName(ctx, testRoomID, ""))
}

func TestSetRoomNameRejectsBlankRoomID(t *testing.T) {
	as := newTestAlarmService(t)

	require.ErrorContains(t, as.SetRoomName(t.Context(), " ", "관리 이름"), "room id is required")
}
