package dbtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSchedulePosterReviewRequestBaselineDoesNotChangeReviewTimes(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	const insert = `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt, created_at)
		VALUES ($1, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg', now(), '', $2)`

	created := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)

	_, err := pool.Exec(ctx, insert, 1, created)
	require.NoError(t, err)

	var requested time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT review_requested_at FROM schedule_posters WHERE id = 1`).Scan(&requested))
	require.WithinDuration(t, time.Now(), requested, time.Minute, "기본값이라 자동 검토를 모르는 삽입은 바로 관리자에게 넘긴 행이 된다")

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET review_requested_at = NULL WHERE id = 1`)
	require.NoError(t, err)

	require.NoError(t, ApplyMigrations(ctx, pool))

	var unset bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT review_requested_at IS NULL FROM schedule_posters WHERE id = 1`).Scan(&unset))
	require.True(t, unset)

	_, err = pool.Exec(ctx, insert, 2, created)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT review_requested_at FROM schedule_posters WHERE id = 2`).Scan(&requested))
	require.WithinDuration(t, time.Now(), requested, time.Minute)
}
