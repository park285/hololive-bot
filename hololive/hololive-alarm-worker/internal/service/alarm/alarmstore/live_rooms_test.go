package alarmstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

func TestLiveRoomsListsDistinctLiveSubscribersExceptPausedRooms(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newAlarmTargetLookupTestDB(t)

	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-live", ChannelID: uncachedChannelA, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-default", ChannelID: uncachedChannelA, AlarmTypes: domain.DefaultAlarmTypes})
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-paused", ChannelID: uncachedChannelA, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-community", ChannelID: uncachedChannelA, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity}})
	requireAlarmRecord(t, db, &domain.Alarm{RoomID: "room-other", ChannelID: uncachedChannelB, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})

	_, err := db.Exec(ctx, `INSERT INTO kakao_room_delivery_pauses (room_id, reason, paused_at) VALUES ('room-paused', 'room_not_joined', now())`)
	require.NoError(t, err)

	rooms := NewLiveRooms(NewSubscriberResolver(testutil.NewTestCacheService(ctx, t), db), kakaoroom.NewDeliveryPauses(db.Pool))

	got, err := rooms.LiveRooms(ctx, uncachedChannelA)
	require.NoError(t, err)
	require.Equal(t, []string{"room-default", "room-live"}, got)

	got, err = rooms.LiveRooms(ctx, uncachedChannelC)
	require.NoError(t, err)
	require.Equal(t, []string{}, got)
}
