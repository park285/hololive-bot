package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestReadySnapshotAndClaimShareExactThresholds(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		parentDelta, dueDelta, lockDelta time.Duration
		locked                           bool
		want                             int64
	}{
		{name: "freshness equality", want: 1},
		{name: "parent one microsecond stale", parentDelta: -time.Microsecond},
		{name: "retry one microsecond future", dueDelta: time.Microsecond},
		{name: "lock equality excluded", locked: true},
		{name: "lock one microsecond expired", locked: true, lockDelta: -time.Microsecond, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx := t.Context()
			at := time.Now().UTC().Truncate(time.Microsecond)
			freshCutoff := at.Add(-2 * time.Hour)
			lockCutoff := at.Add(-time.Minute)

			var parentID int64

			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO youtube_notification_outbox
				(kind,channel_id,content_id,payload,created_at)
				VALUES ('NEW_VIDEO','boundary-channel','boundary-video','{}',$1) RETURNING id`, freshCutoff.Add(tc.parentDelta)).Scan(&parentID))

			var lockedAt *time.Time

			if tc.locked {
				lockedAt = new(lockCutoff.Add(tc.lockDelta))
			}

			_, err := pool.Exec(ctx, `INSERT INTO youtube_notification_delivery
				(outbox_id,room_id,status,next_attempt_at,created_at,locked_at)
				VALUES ($1,'boundary-room','PENDING',$2,$3,$4)`, parentID, at.Add(tc.dueDelta), at.Add(-24*time.Hour), lockedAt)
			require.NoError(t, err)

			var (
				depth      int64
				ageSeconds float64
			)

			require.NoError(t, pool.QueryRow(ctx, readySnapshotSQL, "PENDING", lockCutoff, at, freshCutoff, 10).Scan(&depth, &ageSeconds))
			require.Equal(t, tc.want, depth)
			require.Zero(t, ageSeconds, "due equality, not old delivery creation, owns age")

			rows, err := pool.Query(ctx, claimPendingSQL, "PENDING", lockCutoff, at, freshCutoff, 10, at, "youtube-dispatcher:test")
			require.NoError(t, err)

			claimed, err := pgx.CollectRows(rows, scanTransitionDelivery)
			require.NoError(t, err)
			require.Len(t, claimed, int(tc.want))
		})
	}
}

func TestReadySnapshotUsesExpiredLockAge(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)

	seedTransitionLogicalGroup(t, pool, "expired-lock-age", "expired-lock-room")

	_, err := pool.Exec(ctx, `UPDATE youtube_notification_delivery
		SET next_attempt_at=$1, created_at=$1, locked_at=$2`, at.Add(-time.Hour), at.Add(-time.Minute-time.Second))
	require.NoError(t, err)

	var (
		depth      int64
		ageSeconds float64
	)

	require.NoError(t, pool.QueryRow(ctx, readySnapshotSQL, "PENDING", at.Add(-time.Minute), at, at.Add(-2*time.Hour), 10).Scan(&depth, &ageSeconds))
	require.EqualValues(t, 2, depth)
	require.InDelta(t, 1.0, ageSeconds, 1e-6)
}
