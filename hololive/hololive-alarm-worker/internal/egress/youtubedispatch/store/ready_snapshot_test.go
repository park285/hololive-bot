package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	readyTestLease     = time.Minute
	readyTestFreshness = 2 * time.Hour
)

// 각 경계는 DB 시계 기준 상대값으로 seed한다.
func TestReadySnapshotAndClaimShareThresholds(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		parentAge, dueOffset, lockAge time.Duration
		locked                        bool
		want                          int64
	}{
		{name: "fresh parent", parentAge: readyTestFreshness - time.Minute, want: 1},
		{name: "stale parent", parentAge: readyTestFreshness + time.Minute},
		{name: "retry in the future", parentAge: time.Minute, dueOffset: time.Minute},
		{name: "live lease", parentAge: time.Minute, locked: true, lockAge: readyTestLease - 10*time.Second},
		{name: "expired lease", parentAge: time.Minute, locked: true, lockAge: readyTestLease + time.Second, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx := t.Context()

			var parentID int64

			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO youtube_notification_outbox
				(kind,channel_id,content_id,payload,created_at)
				VALUES ('NEW_VIDEO','boundary-channel','boundary-video','{}',
					clock_timestamp() - $1::bigint * INTERVAL '1 millisecond') RETURNING id`,
				tc.parentAge.Milliseconds()).Scan(&parentID))

			var lockAgeMS *int64

			if tc.locked {
				lockAgeMS = new(tc.lockAge.Milliseconds())
			}

			_, err := pool.Exec(ctx, `INSERT INTO youtube_notification_delivery
				(outbox_id,room_id,status,next_attempt_at,created_at,locked_at)
				VALUES ($1,'boundary-room','PENDING',
					clock_timestamp() + $2::bigint * INTERVAL '1 millisecond',
					clock_timestamp() - INTERVAL '24 hours',
					clock_timestamp() - $3::bigint * INTERVAL '1 millisecond')`,
				parentID, tc.dueOffset.Milliseconds(), lockAgeMS)
			require.NoError(t, err)

			values, err := ReadySnapshot(ctx, pool, readyTestLease, readyTestFreshness, 10)
			require.NoError(t, err)
			require.Equal(t, tc.want, values.Depth)

			rows, err := pool.Query(ctx, transitionClaimPendingSQL, "PENDING", readyTestLease.Milliseconds(),
				readyTestFreshness.Milliseconds(), 10, "youtube-dispatcher:test")
			require.NoError(t, err)

			claimed, err := pgx.CollectRows(rows, scanTransitionDelivery)
			require.NoError(t, err)
			require.Len(t, claimed, int(values.Depth), "claim must agree with the ready snapshot")
		})
	}
}

func TestReadySnapshotUsesExpiredLockAge(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	seedTransitionLogicalGroup(t, pool, "expired-lock-age", "expired-lock-room")

	_, err := pool.Exec(ctx, `UPDATE youtube_notification_delivery
		SET next_attempt_at = clock_timestamp() - INTERVAL '1 hour',
		    created_at = clock_timestamp() - INTERVAL '1 hour',
		    locked_at = clock_timestamp() - INTERVAL '61 seconds'`)
	require.NoError(t, err)

	values, err := ReadySnapshot(ctx, pool, readyTestLease, readyTestFreshness, 10)
	require.NoError(t, err)
	require.EqualValues(t, 2, values.Depth)
	require.InDelta(t, time.Second.Seconds(), values.OldestQueuedAge.Seconds(), 0.5)
}

func TestReadySnapshotExcludesExpiredParents(t *testing.T) {
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

	values, err := ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, values.Depth)
	require.Less(t, values.OldestQueuedAge, time.Minute)

	expired, err := expiredPendingCount(ctx, pool, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, expired)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_notification_delivery (outbox_id,room_id,status,next_attempt_at,created_at)
        SELECT id,'second-expired-room','PENDING',created_at,created_at FROM youtube_notification_outbox WHERE content_id='expired'`)
	require.NoError(t, err)

	expired, err = expiredPendingCount(ctx, pool, 7*24*time.Hour, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, expired, "만료 집계는 전달받은 batch 한도를 넘지 않는다")

	sampled, err := SampleQueue(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.Equal(t, values.Depth, sampled.Depth)
	require.InDelta(t, 2, testutil.ToFloat64(expiredPendingBounded), 0.000001, "표본은 만료 PENDING 수를 gauge에 기록한다")

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET locked_at=clock_timestamp() WHERE outbox_id IN (SELECT id FROM youtube_notification_outbox WHERE content_id='fresh')`)
	require.NoError(t, err)

	values, err = ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.Zero(t, values.Depth)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET locked_at=NULL,next_attempt_at=clock_timestamp()+INTERVAL '1 hour' WHERE outbox_id IN (SELECT id FROM youtube_notification_outbox WHERE content_id='fresh')`)
	require.NoError(t, err)

	values, err = ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.Zero(t, values.Depth)
}

func TestReadySnapshotRequiresCompleteFrozenGroup(t *testing.T) {
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
		values, snapshotErr := ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, tc.batch)
		require.NoError(t, snapshotErr)
		require.Equal(t, tc.depth, values.Depth)
	}

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET next_attempt_at=clock_timestamp()+INTERVAL '1 hour' WHERE id=$1`, members[1])
	require.NoError(t, err)

	values, err := ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, values.Depth, "동일 request의 멤버 일부만 ready로 집계하지 않는다")

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_delivery SET next_attempt_at=clock_timestamp() WHERE id=$1`, members[1])
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_notification_outbox SET created_at=clock_timestamp()-INTERVAL '8 days'
		WHERE id=(SELECT outbox_id FROM youtube_notification_delivery WHERE id=$1)`, members[1])
	require.NoError(t, err)

	values, err = ReadySnapshot(ctx, pool, time.Minute, 7*24*time.Hour, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, values.Depth, "one stale parent excludes the complete frozen group")
}
