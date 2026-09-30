package workerapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestYouTubeReadySnapshotExcludesExpiredParents(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	now := time.Now().UTC()

	for i, created := range []time.Time{now, now.Add(-10 * 24 * time.Hour)} {
		var id int64

		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO youtube_notification_outbox
   (kind,channel_id,content_id,payload,created_at) VALUES ('NEW_VIDEO','snapshot-channel',$1,'{}',$2) RETURNING id`, []string{"fresh", "expired"}[i], created).Scan(&id))

		_, err := pool.Exec(ctx, `INSERT INTO youtube_notification_delivery (outbox_id,room_id,status,next_attempt_at,created_at) VALUES ($1,'snapshot-room','PENDING',$2,$2)`, id, created)
		require.NoError(t, err)
	}

	var (
		depth int64
		age   float64
	)

	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryReadySnapshotSQL, time.Minute.Milliseconds(), (7*24*time.Hour).Milliseconds(), 100).Scan(&depth, &age))
	require.EqualValues(t, 1, depth)
	require.Less(t, age, 60.0)

	var expired int64

	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryExpiredSnapshotSQL, (7*24*time.Hour).Milliseconds(), 100).Scan(&expired))
	require.EqualValues(t, 1, expired)

	_, err := pool.Exec(ctx, `INSERT INTO youtube_notification_delivery (outbox_id,room_id,status,next_attempt_at,created_at)
        SELECT id,'second-expired-room','PENDING',created_at,created_at FROM youtube_notification_outbox WHERE content_id='expired'`)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryExpiredSnapshotSQL, (7*24*time.Hour).Milliseconds(), 1).Scan(&expired))
	require.EqualValues(t, 1, expired, "만료 집계는 전달받은 batch 한도를 넘지 않는다")
	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryExpiredSnapshotSQL, (7*24*time.Hour).Milliseconds(), 100).Scan(&expired))
	require.EqualValues(t, 2, expired)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET locked_at=clock_timestamp() WHERE outbox_id IN (SELECT id FROM youtube_notification_outbox WHERE content_id='fresh')`)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryReadySnapshotSQL, time.Minute.Milliseconds(), (7*24*time.Hour).Milliseconds(), 100).Scan(&depth, &age))
	require.Zero(t, depth)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET locked_at=NULL,next_attempt_at=clock_timestamp()+INTERVAL '1 hour' WHERE outbox_id IN (SELECT id FROM youtube_notification_outbox WHERE content_id='fresh')`)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryReadySnapshotSQL, time.Minute.Milliseconds(), (7*24*time.Hour).Milliseconds(), 100).Scan(&depth, &age))
	require.Zero(t, depth)
}

func TestYouTubeReadySnapshotRequiresCompleteFrozenGroup(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	members := make([]int64, 0, 2)

	for _, content := range []string{"group-a", "group-b", "single"} {
		var outboxID, deliveryID int64

		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO youtube_notification_outbox
   (kind,channel_id,content_id,payload) VALUES ('NEW_VIDEO','ready-group-channel',$1,'{}') RETURNING id`, content).Scan(&outboxID))
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO youtube_notification_delivery
   (outbox_id,room_id,status) VALUES ($1,'ready-group-room','PENDING') RETURNING id`, outboxID).Scan(&deliveryID))

		if content != "single" {
			members = append(members, deliveryID)
		}
	}

	_, err := pool.Exec(ctx, `INSERT INTO youtube_notification_send_request
  (base_id,room_id,message,message_hash,route,dedupe_keys,member_ids)
  VALUES ('ready-group-request','ready-group-room','fixed',repeat('a',64),'text',ARRAY['key-a','key-b'],$1)`, members)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET send_request_id='ready-group-request' WHERE id=ANY($1)`, members)
	require.NoError(t, err)

	for _, tc := range []struct {
		batch int
		depth int64
	}{{1, 1}, {2, 3}} {
		var (
			depth int64
			age   float64
		)

		require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryReadySnapshotSQL, time.Minute.Milliseconds(), (7*24*time.Hour).Milliseconds(), tc.batch).Scan(&depth, &age))
		require.Equal(t, tc.depth, depth)
	}

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET next_attempt_at=clock_timestamp()+INTERVAL '1 hour' WHERE id=$1`, members[1])
	require.NoError(t, err)

	var (
		depth int64
		age   float64
	)

	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryReadySnapshotSQL, time.Minute.Milliseconds(), (7*24*time.Hour).Milliseconds(), 100).Scan(&depth, &age))
	require.EqualValues(t, 1, depth, "동일 request의 멤버 일부만 ready로 집계하지 않는다")
}
