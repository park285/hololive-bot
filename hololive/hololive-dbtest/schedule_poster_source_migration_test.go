package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulePosterSourceMigrationKeepsConsoleURLShapes(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	const insert = `INSERT INTO schedule_posters (id, source, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
		VALUES ($1, $2, 'UC8eitCE9Z6EwUCs-VUi1blg', $3, $4, $5, now(), '')`

	_, err := pool.Exec(ctx, `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
		VALUES (1, 'UC8eitCE9Z6EwUCs-VUi1blg', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg', now(), '')`)
	require.NoError(t, err, "출처를 모르는 이전 image의 X 후보 삽입은 기본값 x로 들어간다")

	_, err = pool.Exec(ctx, insert, 2, "youtube_community", "", "https://www.youtube.com/post/UgkxSchedule", "https://yt3.ggpht.com/poster=s0")
	require.NoError(t, err)

	for index, row := range []struct{ source, user, post, image string }{
		{"x", "1", "https://x.com/i/status/3", "https://yt3.ggpht.com/poster=s0"},
		{"youtube_community", "1", "https://www.youtube.com/post/UgkxSchedule", "https://yt3.ggpht.com/poster=s0"},
		{"youtube_community", "", "https://www.youtube.com/post/Ugkx/../a", "https://yt3.ggpht.com/poster=s0"},
		{"rss", "", "https://www.youtube.com/post/UgkxSchedule", "https://yt3.ggpht.com/poster=s0"},
	} {
		_, err := pool.Exec(ctx, insert, index+10, row.source, row.user, row.post, row.image)
		require.ErrorContains(t, err, "chk_schedule_posters_source", "%+v", row)
	}
}
