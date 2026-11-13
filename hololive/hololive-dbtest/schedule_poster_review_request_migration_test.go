package dbtest

import (
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const schedulePosterReviewRequestMigration = "017_schedule_poster_review_request.sql"

func TestSchedulePosterReviewRequestMigrationBackfillsCreatedAt(t *testing.T) {
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

	_, err = pool.Exec(ctx, `ALTER TABLE schedule_posters ALTER COLUMN review_requested_at DROP DEFAULT`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET review_requested_at = NULL WHERE id = 1`)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), schedulePosterReviewRequestMigration)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterReviewRequestMigration, string(migration)))

	require.NoError(t, pool.QueryRow(ctx, `SELECT review_requested_at FROM schedule_posters WHERE id = 1`).Scan(&requested))
	require.True(t, requested.Equal(created), "기존 행은 생성 시각으로 채워 Console 최신 후보가 배포로 바뀌지 않는다")

	_, err = pool.Exec(ctx, insert, 2, created)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT review_requested_at FROM schedule_posters WHERE id = 2`).Scan(&requested))
	require.WithinDuration(t, time.Now(), requested, time.Minute)
}
