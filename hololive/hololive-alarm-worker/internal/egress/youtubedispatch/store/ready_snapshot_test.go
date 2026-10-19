package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	readyTestLease     = time.Minute
	readyTestFreshness = 2 * time.Hour
)

// ReadySnapshot과 ClaimPending은 같은 predicate와 DB 시계로 claim 가능성을 판정한다. 각 경계는 DB 시계 기준
// 상대값으로 seed한다.
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

			rows, err := pool.Query(ctx, claimPendingSQL, "PENDING", readyTestLease.Milliseconds(),
				readyTestFreshness.Milliseconds(), 10, "youtube-dispatcher:test")
			require.NoError(t, err)

			claimed, err := pgx.CollectRows(rows, scanTransitionDelivery)
			require.NoError(t, err)
			require.Len(t, claimed, int(values.Depth), "claim must agree with the ready snapshot")
		})
	}
}

// 만료된 lease 행의 대기 시간은 next_attempt_at이 아니라 lease 만료(locked_at + lease)부터 센다.
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
