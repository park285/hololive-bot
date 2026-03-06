package notification

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
)

// -- AddAlarm 테스트 --

func TestAddAlarm_CacheWrite(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	ctx := context.Background()

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

	// 알람 키에 채널 ID가 추가되었는지 확인
	channels, err := as.cache.SMembers(ctx, AlarmKeyPrefix+"room1")
	require.NoError(t, err)
	assert.Contains(t, channels, "UC_TEST")

	// 레지스트리에 등록되었는지 확인
	registry, err := as.cache.SMembers(ctx, AlarmRegistryKey)
	require.NoError(t, err)
	assert.Contains(t, registry, "room1")

	// 채널 레지스트리에 등록되었는지 확인
	channelReg, err := as.cache.SMembers(ctx, AlarmChannelRegistryKey)
	require.NoError(t, err)
	assert.Contains(t, channelReg, "UC_TEST")

	// 멤버 이름 캐시 확인
	name, err := as.cache.HGet(ctx, MemberNameKey, "UC_TEST")
	require.NoError(t, err)
	assert.Equal(t, "테스트 멤버", name)
}

func TestAddAlarm_DuplicateReturnsNotAdded(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	ctx := context.Background()

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

// -- RemoveAlarm 테스트 --

func TestRemoveAlarm_Success(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	ctx := context.Background()

	// 알람 추가
	req := domain.AddAlarmRequest{
		RoomID:     "room1",
		ChannelID:  "UC_TEST",
		MemberName: "멤버",
	}
	_, err := as.AddAlarm(ctx, req)
	require.NoError(t, err)

	// 알람 제거
	removed, err := as.RemoveAlarm(ctx, "room1", "UC_TEST", nil)
	require.NoError(t, err)
	assert.True(t, removed)

	// 제거 후 비어있는지 확인
	channels, err := as.cache.SMembers(ctx, AlarmKeyPrefix+"room1")
	require.NoError(t, err)
	assert.Empty(t, channels)
}

func TestRemoveAlarm_NotFound(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	ctx := context.Background()

	removed, err := as.RemoveAlarm(ctx, "room1", "UC_NONEXIST", nil)
	require.NoError(t, err)
	assert.False(t, removed)
}

// -- GetRoomAlarms 테스트 --

func TestGetRoomAlarms_WithAlarms(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	ctx := context.Background()

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
	ctx := context.Background()

	channels, err := as.GetRoomAlarms(ctx, "room_empty")
	require.NoError(t, err)
	assert.Empty(t, channels)
}

// -- ClearRoomAlarms 테스트 --

func TestClearRoomAlarms_ClearsAll(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	ctx := context.Background()

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

	// 방 알람이 비어있는지 확인
	channels, err := as.GetRoomAlarms(ctx, "room1")
	require.NoError(t, err)
	assert.Empty(t, channels)

	// 레지스트리에서 제거되었는지 확인
	registry, err := as.cache.SMembers(ctx, AlarmRegistryKey)
	require.NoError(t, err)
	assert.NotContains(t, registry, "room1")
}

func TestClearRoomAlarms_EmptyRoom(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()

	cleared, err := as.ClearRoomAlarms(ctx, "room_empty")
	require.NoError(t, err)
	assert.Equal(t, 0, cleared)
}

// -- MarkAsNotified 테스트 --

func TestMarkAsNotified_SetsFlag(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()

	start := time.Now().UTC().Truncate(time.Minute)
	err := as.MarkAsNotified(ctx, "stream1", start, 5)
	require.NoError(t, err)

	// 기록된 데이터 확인
	var data NotifiedData
	err = as.cache.Get(ctx, NotifiedKeyPrefix+"stream1", &data)
	require.NoError(t, err)
	assert.True(t, data.SentAt[5])
	assert.Equal(t, start.Format(time.RFC3339), data.StartScheduled)
}

func TestMarkAsNotified_ScheduleChangeResetsMap(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()

	start1 := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	start2 := time.Date(2026, 3, 2, 11, 0, 0, 0, time.UTC)

	// 첫 번째 스케줄
	err := as.MarkAsNotified(ctx, "stream1", start1, 5)
	require.NoError(t, err)

	// 스케줄 변경
	err = as.MarkAsNotified(ctx, "stream1", start2, 3)
	require.NoError(t, err)

	var data NotifiedData
	err = as.cache.Get(ctx, NotifiedKeyPrefix+"stream1", &data)
	require.NoError(t, err)

	// 이전 5분 마킹은 리셋되어야 함
	assert.False(t, data.SentAt[5])
	assert.True(t, data.SentAt[3])
	assert.Equal(t, start2.Format(time.RFC3339), data.StartScheduled)
}

// -- GetTargetMinutes / UpdateAlarmAdvanceMinutes --

func TestGetTargetMinutes_Default(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	got := as.GetTargetMinutes()
	assert.Equal(t, []int{30, 15, 5, 1}, got) // newTestAlarmService에서 설정
}

func TestUpdateAlarmAdvanceMinutes(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	result := as.UpdateAlarmAdvanceMinutes(20)

	// 20, 3, 1이 정규화됨
	assert.Contains(t, result, 20)
	assert.Contains(t, result, 1)

	got := as.GetTargetMinutes()
	assert.Equal(t, result, got)
}

// -- CacheMemberName / GetMemberName --

func TestCacheMemberName_RoundTrip(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()

	err := as.CacheMemberName(ctx, "UC_TEST", "페코라")
	require.NoError(t, err)

	name, err := as.GetMemberName(ctx, "UC_TEST")
	require.NoError(t, err)
	assert.Equal(t, "페코라", name)
}

// -- SetRoomName / SetUserName --

func TestSetRoomName(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()

	err := as.SetRoomName(ctx, "room1", "테스트 방")
	require.NoError(t, err)

	name, err := as.cache.HGet(ctx, RoomNamesCacheKey, "room1")
	require.NoError(t, err)
	assert.Equal(t, "테스트 방", name)
}

func TestSetUserName(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()

	err := as.SetUserName(ctx, "user1", "테스트 사용자")
	require.NoError(t, err)

	name, err := as.cache.HGet(ctx, UserNamesCacheKey, "user1")
	require.NoError(t, err)
	assert.Equal(t, "테스트 사용자", name)
}

// -- GetAllAlarmKeys --

func TestGetAllAlarmKeys(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	ctx := context.Background()

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

// -- GetDistinctRooms --

func TestGetDistinctRooms(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}
	ctx := context.Background()

	_, _ = as.AddAlarm(ctx, domain.AddAlarmRequest{RoomID: "room1", ChannelID: "UC_A"})
	_, _ = as.AddAlarm(ctx, domain.AddAlarmRequest{RoomID: "room2", ChannelID: "UC_B"})

	rooms, err := as.GetDistinctRooms(ctx)
	require.NoError(t, err)
	assert.Len(t, rooms, 2)
}

// -- Close --

func TestAlarmServiceClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cacheSvc := newTestCacheService(t, ctx)

	svc, err := NewAlarmService(cacheSvc, nil, nil, nil, nil, nil, nil, []int{5, 3, 1})
	require.NoError(t, err)

	// 두 번 호출해도 안전해야 함 (closeOnce)
	err = svc.Close(ctx)
	assert.NoError(t, err)
	err = svc.Close(ctx)
	assert.NoError(t, err)
}

// -- WarmCacheFromDB: repo nil인 경우 --

func TestWarmCacheFromDB_NilRepo(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()

	// alarmRepo가 nil이면 스킵
	err := as.WarmCacheFromDB(ctx)
	assert.NoError(t, err)
}

// -- persistence async: 풀이 nil인 경우 --

func TestSubmitPersistTask_NilPool(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	// persistPool이 nil이면 로그만 남기고 패닉 안 함
	as.submitPersistTask("test", func() { t.Fatal("should not be called") })
}

func TestSubmitPersistTask_ClosedPool(t *testing.T) {
	t.Parallel()

	pool, err := workerpool.New(workerpool.DefaultConfig())
	require.NoError(t, err)
	pool.Shutdown()

	called := false
	as := &AlarmService{
		persistPool: pool,
		logger:      newDiscardAlarmLogger(),
	}
	as.submitPersistTask("test", func() { called = true })

	assert.False(t, called)
}
