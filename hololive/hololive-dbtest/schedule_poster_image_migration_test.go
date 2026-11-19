package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const mimeJPEG = "image/jpeg"

func TestSchedulePosterImageMigrationKeepsBytesAndMIMETogether(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt)
		VALUES (1, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg', now(), '')`)
	require.NoError(t, err, "그림 열을 모르는 이전 image의 삽입은 두 열이 NULL로 들어간다")

	_, err = pool.Exec(ctx, `INSERT INTO schedule_posters (id, source, channel_id, x_user_id, post_url, image_url, image_sha256,
		image_data, image_mime, posted_at, text_excerpt)
		VALUES (2, 'youtube_thumbnail', 'UC8eitCE9Z6EwUCs-VUi1blg', '', 'https://www.youtube.com/watch?v=4uuYjSf0rpU',
		        'https://i.ytimg.com/vi/4uuYjSf0rpU/maxresdefault.jpg', encode(sha256('\x01'::bytea), 'hex'), '\x01'::bytea, 'image/jpeg', now(), '')`)
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, ApplyMigrations(ctx, pool), "다시 적용해도 실패하지 않는다")
	}

	var unstored, stored int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE image_data IS NULL AND image_mime IS NULL),
		count(*) FILTER (WHERE image_data = '\x01'::bytea AND image_mime = 'image/jpeg') FROM schedule_posters`).Scan(&unstored, &stored))
	require.Equal(t, 1, unstored, "기존 행은 NULL로 남는다")
	require.Equal(t, 1, stored, "다시 적용해도 저장한 그림을 지우지 않는다")

	for name, values := range map[string]string{
		"바이트만":   `image_data = '\x01'::bytea`,
		"MIME만":  `image_mime = 'image/jpeg'`,
		"빈 바이트":  `image_data = ''::bytea, image_mime = 'image/jpeg'`,
		"빈 MIME": `image_data = '\x01'::bytea, image_mime = ''`,
		"모르는 형식": `image_data = '\x01'::bytea, image_mime = 'image/gif'`,
	} {
		_, err := pool.Exec(ctx, `UPDATE schedule_posters SET `+values+` WHERE id = 1`)
		require.ErrorContains(t, err, "chk_schedule_posters_image", name)
	}

	for _, mime := range []string{mimeJPEG, "image/png", "image/webp"} {
		_, err := pool.Exec(ctx, `UPDATE schedule_posters SET image_data = '\x01'::bytea, image_mime = $1 WHERE id = 1`, mime)
		require.NoError(t, err, mime)
	}
}
