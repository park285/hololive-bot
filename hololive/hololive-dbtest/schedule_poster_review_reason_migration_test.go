package dbtest

import (
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const schedulePosterReviewReasonMigration = "018_schedule_poster_review_request_reason.sql"

func TestSchedulePosterReviewReasonMigrationBackfillsTheReviewer(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt,
		review_status, week_start, week_end, review_requested_at, reviewed_by)
		VALUES (1, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg', now(), '', 'confirmed', '2026-11-09', '2026-11-15', now(), NULL),
		       (2, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/2', 'https://pbs.twimg.com/media/b.jpg', now(), '', 'rejected', NULL, NULL, NULL, NULL),
		       (3, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/3', 'https://pbs.twimg.com/media/c.jpg', now(), '', 'pending_review', NULL, NULL, NULL, NULL),
		       (4, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/4', 'https://pbs.twimg.com/media/d.jpg', now(), '', 'rejected', NULL, NULL, now(), 'auto')`)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), schedulePosterReviewReasonMigration)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterReviewReasonMigration, string(migration)))

	rows, err := pool.Query(ctx, `SELECT COALESCE(reviewed_by, '') FROM schedule_posters ORDER BY id`)
	require.NoError(t, err)

	var reviewers []string

	for rows.Next() {
		var reviewer string

		require.NoError(t, rows.Scan(&reviewer))

		reviewers = append(reviewers, reviewer)
	}

	require.NoError(t, rows.Err())
	require.Equal(t, []string{"admin", "auto", "", "auto"}, reviewers, "관리자에게 넘긴 적이 있는 판정은 admin, 없으면 auto이고 이미 채운 값은 둔다")

	for name, update := range map[string]string{
		"넘기지 않은 행의 이유": `UPDATE schedule_posters SET review_request_reason = 'uncertain' WHERE id = 3`,
		"모르는 이유":       `UPDATE schedule_posters SET review_request_reason = 'unknown' WHERE id = 1`,
		"모르는 판정 주체":    `UPDATE schedule_posters SET reviewed_by = 'bot' WHERE id = 1`,
	} {
		_, err = pool.Exec(ctx, update)
		require.Error(t, err, name)
	}

	_, err = pool.Exec(ctx, `UPDATE schedule_posters SET review_request_reason = 'stale' WHERE id = 1`)
	require.NoError(t, err)
}
