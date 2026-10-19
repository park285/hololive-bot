package workerapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
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

	values, err := store.ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, values.Depth)
	require.Less(t, values.OldestQueuedAge, time.Minute)

	var expired int64

	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryExpiredSnapshotSQL, (7*24*time.Hour).Milliseconds(), 100).Scan(&expired))
	require.EqualValues(t, 1, expired)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_notification_delivery (outbox_id,room_id,status,next_attempt_at,created_at)
        SELECT id,'second-expired-room','PENDING',created_at,created_at FROM youtube_notification_outbox WHERE content_id='expired'`)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryExpiredSnapshotSQL, (7*24*time.Hour).Milliseconds(), 1).Scan(&expired))
	require.EqualValues(t, 1, expired, "만료 집계는 전달받은 batch 한도를 넘지 않는다")
	require.NoError(t, pool.QueryRow(ctx, youtubeDeliveryExpiredSnapshotSQL, (7*24*time.Hour).Milliseconds(), 100).Scan(&expired))
	require.EqualValues(t, 2, expired)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET locked_at=clock_timestamp() WHERE outbox_id IN (SELECT id FROM youtube_notification_outbox WHERE content_id='fresh')`)
	require.NoError(t, err)

	values, err = store.ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.Zero(t, values.Depth)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET locked_at=NULL,next_attempt_at=clock_timestamp()+INTERVAL '1 hour' WHERE outbox_id IN (SELECT id FROM youtube_notification_outbox WHERE content_id='fresh')`)
	require.NoError(t, err)

	values, err = store.ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.Zero(t, values.Depth)
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
		values, snapshotErr := store.ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, tc.batch)
		require.NoError(t, snapshotErr)
		require.Equal(t, tc.depth, values.Depth)
	}

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET next_attempt_at=clock_timestamp()+INTERVAL '1 hour' WHERE id=$1`, members[1])
	require.NoError(t, err)

	values, err := store.ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, values.Depth, "동일 request의 멤버 일부만 ready로 집계하지 않는다")

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET next_attempt_at=clock_timestamp() WHERE id=$1`, members[1])
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_outbox SET created_at=clock_timestamp()-INTERVAL '8 days'
		WHERE id=(SELECT outbox_id FROM youtube_notification_delivery WHERE id=$1)`, members[1])
	require.NoError(t, err)

	values, err = store.ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, values.Depth, "one stale parent excludes the complete frozen group")
}
