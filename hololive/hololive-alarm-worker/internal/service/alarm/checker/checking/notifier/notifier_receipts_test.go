package notifier

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNotifierMixedCollisionOnlyMarksAcceptedRooms(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, logger)
	publisher := queue.NewPublisher(cache, logger, queue.WithOutbox(repository), queue.WithWakeupEnabled(false))
	service := dedup.NewService(cache, []int{5}, logger)
	n, err := NewNotifier(service, publisher, tier.NewTieredScheduler(logger), logger)
	require.NoError(t, err)

	start := time.Now().UTC().Add(5 * time.Minute)
	first := newNotifierPublishTestItem("room-first", "receipt-stream", "receipt-channel", start, 5, nil)
	result, err := n.Send(ctx, []*domain.AlarmNotification{first.payload.notification})
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)

	collision := newNotifierPublishTestItem("room-collision", "receipt-stream", "receipt-channel", start, 5, nil)

	collision.payload.notification.Stream.Title = "changed title"

	accepted := newNotifierPublishTestItem("room-accepted", "receipt-stream", "receipt-channel", start, 5, nil)

	result, err = n.Send(ctx, []*domain.AlarmNotification{collision.payload.notification, accepted.payload.notification})
	require.Error(t, err)
	require.Equal(t, 1, result.Sent)
	require.Equal(t, 1, result.Failed)

	for _, item := range []claimedSend{collision, accepted} {
		marked, markErr := service.WasUpcomingEventNotifiedRecently(ctx, item.payload.notification.RoomID, item.payload.channelID, item.payload.notification.Stream, time.Hour)
		require.NoError(t, markErr)
		require.Equal(t, item.payload.notification.RoomID == "room-accepted", marked)
	}

	_, claimed := claimNotificationPair(t, n, collision.payload)
	require.True(t, claimed, "collision claim must be released")

	_, claimed = claimNotificationPair(t, n, accepted.payload)
	require.False(t, claimed, "accepted claim must remain")

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-collision'`).Scan(&count))
	require.Zero(t, count)
}

func TestNotifierPartialCommitRecoversOnlyMissingRoomAndSentReplay(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, logger)
	publisher := queue.NewPublisher(cache, logger, queue.WithOutbox(repository), queue.WithWakeupEnabled(false), queue.WithMaxDeliveriesPerBatch(1))
	service := dedup.NewService(cache, []int{5}, logger)
	n, err := NewNotifier(service, publisher, tier.NewTieredScheduler(logger), logger)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `CREATE FUNCTION reject_second_room() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.room_id='room-second' THEN RAISE EXCEPTION 'injected chunk failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_second_room BEFORE INSERT ON alarm_dispatch_deliveries FOR EACH ROW EXECUTE FUNCTION reject_second_room()`)
	require.NoError(t, err)

	start := time.Now().UTC().Add(5 * time.Minute)
	first := newNotifierPublishTestItem("room-first", "partial-stream", "partial-channel", start, 5, nil)
	second := newNotifierPublishTestItem("room-second", "partial-stream", "partial-channel", start, 5, nil)
	notifications := []*domain.AlarmNotification{first.payload.notification, second.payload.notification}
	result, err := n.Send(ctx, notifications)
	require.ErrorContains(t, err, "injected chunk failure")
	require.Equal(t, 1, result.Sent)
	require.Equal(t, 1, result.Failed)

	for _, item := range []claimedSend{first, second} {
		marked, markErr := service.WasUpcomingEventNotifiedRecently(ctx, item.payload.notification.RoomID, item.payload.channelID, item.payload.notification.Stream, time.Hour)
		require.NoError(t, markErr)
		require.Equal(t, item.payload.notification.RoomID == "room-first", marked)
	}

	_, err = pool.Exec(ctx, `DROP TRIGGER reject_second_room ON alarm_dispatch_deliveries`)
	require.NoError(t, err)

	result, err = n.Send(ctx, notifications)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.Equal(t, 1, result.Skipped)

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries`).Scan(&count))
	require.Equal(t, 2, count)

	// cache 전체가 사라져도 SENT ledger가 새 delivery 및 재발송을 막는다.
	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET status='sent', sent_at=NOW() WHERE room_id='room-first'`)
	require.NoError(t, err)

	replacementCache := newCheckerTestCacheClient(t)
	replacementService := dedup.NewService(replacementCache, []int{5}, logger)
	replay, err := NewNotifier(replacementService, publisher, tier.NewTieredScheduler(logger), logger)
	require.NoError(t, err)

	result, err = replay.Send(ctx, []*domain.AlarmNotification{first.payload.notification})
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries WHERE room_id='room-first' AND status='sent'`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries`).Scan(&count))
	require.Equal(t, 2, count)
}
