package dbtest

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpcomingCandidatesMigrationResumesAndPreservesRows(t *testing.T) {
	pool := NewBlankPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const filename = "250_alarm_upcoming_candidates.sql"

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	raw, err := root.ReadFile(filename)
	require.NoError(t, err)

	firstStatement, _, ok := strings.Cut(string(raw), ";")
	require.True(t, ok)

	// autocommit 파일 첫 DDL 완료 뒤 중단한 상태를 실제 테이블로 재현한다.
	_, err = pool.Exec(t.Context(), firstStatement)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `INSERT INTO alarm_upcoming_candidates
		(dedupe_key,event_key,payload_hash,channel_id,stream_id,room_id,scheduled_at,notification,selected_at,checked_at)
		VALUES ('migration-key','event','hash','channel','stream','room',now()+interval '5 minutes','{"minutes_until":5}',now(),now())`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(t.Context(), pool, dir, filename))

	_, err = pool.Exec(t.Context(), "INSERT INTO alarm_upcoming_checkpoints(channel_id,evaluated_at) VALUES ('channel',now())")
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(t.Context(), pool, dir, filename))

	var outcome string

	var minutes, checkpoints int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT outcome,(notification->>'minutes_until')::int FROM alarm_upcoming_candidates WHERE dedupe_key='migration-key'").Scan(&outcome, &minutes))
	require.Equal(t, "pending", outcome)
	require.Equal(t, 5, minutes)
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_upcoming_checkpoints").Scan(&checkpoints))
	require.Equal(t, 1, checkpoints)

	for range 2 {
		for _, index := range []string{"252_alarm_upcoming_pending_index.sql", "253_alarm_upcoming_terminal_index.sql"} {
			require.NoError(t, applyMigrationFile(t.Context(), pool, dir, index))
		}
	}
}
