package dbtest

import (
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const schedulePosterNotFoundMigration = "023_schedule_poster_not_found_text.sql"

func TestSchedulePosterNotFoundMigrationRewritesSeededTextOnly(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	const (
		seeded  = "이번 주 번역된 일정 포스터가 아직 없습니다."
		current = "이번 주 일정표가 아직 없습니다."
		custom  = "이번 주 포스터는 아직이에요."
	)

	load := func() string {
		var value string

		require.NoError(t, pool.QueryRow(ctx, `SELECT value FROM message_strings
			WHERE namespace = 'misc' AND key = 'schedule_poster_not_found'`).Scan(&value))

		return value
	}

	set := func(value string) {
		_, err := pool.Exec(ctx, `UPDATE message_strings SET value = $1
			WHERE namespace = 'misc' AND key = 'schedule_poster_not_found'`, value)
		require.NoError(t, err)
	}

	require.Equal(t, current, load())

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), schedulePosterNotFoundMigration)
	require.NoError(t, err)

	apply := func() {
		for range 2 {
			require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterNotFoundMigration, string(migration)))
		}
	}

	set(seeded)
	apply()
	require.Equal(t, current, load())

	set(custom)
	apply()
	require.Equal(t, custom, load())
}
