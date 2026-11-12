package dbtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulePosterSourceMigrationsKeepConsoleURLShapes(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	const communityImage = "https://yt3.ggpht.com/poster=s0"

	const insert = `INSERT INTO schedule_posters (id, source, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
		VALUES ($1, $2, 'UC8eitCE9Z6EwUCs-VUi1blg', $3, $4, $5, now(), '')`

	_, err := pool.Exec(ctx, `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
		VALUES (1, 'UC8eitCE9Z6EwUCs-VUi1blg', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg', now(), '')`)
	require.NoError(t, err, "출처를 모르는 이전 image의 X 후보 삽입은 기본값 x로 들어간다")

	_, err = pool.Exec(ctx, insert, 2, "youtube_community", "", "https://www.youtube.com/post/UgkxSchedule", communityImage)
	require.NoError(t, err)

	for index, row := range []struct{ source, user, post, image string }{
		{"x", "1", "https://x.com/i/status/3", communityImage},
		{"youtube_community", "1", "https://www.youtube.com/post/UgkxSchedule", communityImage},
		{"youtube_community", "", "https://www.youtube.com/post/Ugkx/../a", communityImage},
		{"rss", "", "https://www.youtube.com/post/UgkxSchedule", communityImage},
	} {
		_, err = pool.Exec(ctx, insert, index+10, row.source, row.user, row.post, row.image)
		require.ErrorContains(t, err, "chk_schedule_posters_source", "%+v", row)
	}

	const thumbnail = `INSERT INTO schedule_posters (id, source, channel_id, x_user_id, post_url, image_url, image_sha256, posted_at, text_excerpt)
		VALUES ($1, $2, 'UC8eitCE9Z6EwUCs-VUi1blg', '', $3, $4, $5, now(), '')`

	hash := strings.Repeat("a", 64)

	_, err = pool.Exec(ctx, thumbnail, 20, "youtube_thumbnail", "https://www.youtube.com/watch?v=4uuYjSf0rpU",
		"https://i.ytimg.com/vi/4uuYjSf0rpU/maxresdefault.jpg", hash)
	require.NoError(t, err)

	for index, row := range []struct {
		source, post, image string
		hash                *string
	}{
		{"youtube_thumbnail", "https://www.youtube.com/watch?v=4uuYjSf0rpU", "https://i.ytimg.com/vi/4uuYjSf0rpU/maxresdefault.jpg", nil},
		{"youtube_thumbnail", "https://www.youtube.com/watch?v=4uuYjSf0rpU", "https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg", &hash},
		{"youtube_thumbnail", "https://www.youtube.com/watch?v=4uuYjSf0rpU&t=1", "https://i.ytimg.com/vi/4uuYjSf0rpU/maxresdefault.jpg", &hash},
		{"youtube_thumbnail", "https://www.youtube.com/watch?v=4uuYjSf0rpU", "https://i.ytimg.com/vi/4uuYjSf0rpU/maxresdefault.jpg", new("ABC")},
		{"youtube_community", "https://www.youtube.com/post/UgkxSchedule", communityImage, &hash},
	} {
		_, err = pool.Exec(ctx, thumbnail, index+30, row.source, row.post, row.image, row.hash)
		require.ErrorContains(t, err, "chk_schedule_posters_source", "%+v", row)
	}
}
