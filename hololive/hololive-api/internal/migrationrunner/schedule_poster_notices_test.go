package migrationrunner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/scripts/migrations"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestSchedulePosterNoticesStartSettledForWhatAlreadyExists(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewBlankPool(t)

	_, err := Run(ctx, pool, migrationsThrough(t, "008"), Config{})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt,
		review_status, week_start, week_end, reviewed_at, reviewed_by, rows_status, rows_extracted_at)
		VALUES (1, 'UC_a', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg', now(), '', 'confirmed', '2026-11-09', '2026-11-15',
		        now(), 'admin', 'succeeded', now()),
		       (2, 'UC_a', '1', 'https://x.com/i/status/2', 'https://pbs.twimg.com/media/b.jpg', now(), '', 'confirmed', '2026-11-16', '2026-11-22',
		        now(), 'admin', 'pending', NULL)`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body, status, schedule_info, kind, matched_poster_id, matched_rows_at)
		VALUES (11, 'UC_a', now(), 'a', 'matched', 0.9, 'change', 1, now()),
		       (12, 'UC_a', now(), 'b', 'relevant', 0.9, 'change', NULL, NULL)`)
	require.NoError(t, err)

	result, err := Run(ctx, pool, migrations.FS, Config{})
	require.NoError(t, err)
	require.Equal(t, 1, result.Applied)

	rows, err := pool.Query(ctx, `SELECT kind, poster_id, COALESCE(post_id, 0), status, settled_at IS NOT NULL FROM schedule_poster_notices ORDER BY kind, poster_id`)
	require.NoError(t, err)

	type notice struct {
		kind    string
		poster  int64
		post    int64
		status  string
		settled bool
	}

	var got []notice

	for rows.Next() {
		var item notice

		require.NoError(t, rows.Scan(&item.kind, &item.poster, &item.post, &item.status, &item.settled))

		got = append(got, item)
	}

	require.NoError(t, rows.Err())
	require.Equal(t, []notice{
		{kind: "changed", poster: 1, post: 11, status: "baseline", settled: true},
		{kind: "published", poster: 1, status: "baseline", settled: true},
	}, got, "이미 뽑힌 포스터와 반영된 글은 배포 직후 다시 알리지 않는다")
}
