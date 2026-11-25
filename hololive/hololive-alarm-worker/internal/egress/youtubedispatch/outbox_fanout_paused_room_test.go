package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFanoutMaterializationSkipsPausedRooms(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	const pausedRoom, activeRoom = "room-fanout-paused", "room-fanout-active"

	_, err := db.Exec(ctx, `INSERT INTO kakao_room_delivery_pauses (room_id, reason, paused_at) VALUES ($1, 'room_not_joined', now())`, pausedRoom)
	require.NoError(t, err)

	item := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewShort,
		ChannelID:     "UC_paused_room",
		ContentID:     "short-paused-room",
		Payload:       `{"video_id":"short-paused","title":"short-title"}`,
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: time.Now(),
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)

	dispatcher := newDispatcherForTest(t, db, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.DiscardHandler), &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})

	claimed, err := dispatcher.claim.fanout.Claim(ctx)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	stats := dispatcher.claim.fanout.materializeOne(ctx, &claimed[0], map[string]channelAlarmRoomTargets{
		item.ChannelID: {domain.AlarmTypeShorts: {pausedRoom: true, activeRoom: true}},
	})
	assert.Equal(t, outboxEnqueueStats{enqueuedOutboxes: 1, totalTargetRooms: 1, pausedRooms: 1}, stats)

	var rows []deliveryTestDeliveryModel

	require.NoError(t, findDeliveryTestRowsOrdered(db, &rows, "room_id ASC").Error)
	require.Len(t, rows, 1)
	assert.Equal(t, activeRoom, rows[0].RoomID)
}
