package dbtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLiveFactClockMigrationDoesNotInventLegacyEvidence(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()
	legacySeen := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
	_, err := pool.Exec(ctx, `ALTER TABLE youtube_live_sessions DROP COLUMN status_observed_at,
 DROP COLUMN schedule_observed_at;
 ALTER TABLE youtube_live_sessions ADD COLUMN status_observed_at TIMESTAMPTZ`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,last_seen_at,scheduled_start_time,is_premiere)
 VALUES('clock-legacy','clock-channel','UPCOMING',$1,$1,true)`, legacySeen)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const file = "254_youtube_live_fact_observation_clocks.sql"

	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))

	var (
		seen                 time.Time
		statusAt, scheduleAt *time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT last_seen_at,status_observed_at,schedule_observed_at
 FROM youtube_live_sessions WHERE video_id='clock-legacy'`).Scan(&seen, &statusAt, &scheduleAt))
	require.True(t, legacySeen.Equal(seen))
	require.Nil(t, statusAt)
	require.Nil(t, scheduleAt)

	observed := legacySeen.Add(-2 * time.Hour)

	_, err = pool.Exec(ctx, `UPDATE youtube_live_sessions SET schedule_observed_at=$1 WHERE video_id='clock-legacy'`, observed)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, file))

	var retained time.Time

	require.NoError(t, pool.QueryRow(ctx, "SELECT schedule_observed_at FROM youtube_live_sessions WHERE video_id='clock-legacy'").Scan(&retained))
	require.True(t, observed.Equal(retained))
}
