package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulePosterNotFoundBaselinePreservesPostInstallText(t *testing.T) {
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

	apply := func() {
		for range 2 {
			require.NoError(t, ApplyMigrations(ctx, pool))
		}
	}

	set(seeded)
	apply()
	require.Equal(t, seeded, load())

	set(custom)
	apply()
	require.Equal(t, custom, load())
}
