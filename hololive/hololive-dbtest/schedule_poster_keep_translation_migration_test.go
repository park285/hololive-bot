package dbtest

import (
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const schedulePosterKeepTranslationMigration = "012_schedule_poster_keep_translation.sql"

func TestSchedulePosterKeepTranslationMigrationConstraints(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), schedulePosterKeepTranslationMigration)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, schedulePosterKeepTranslationMigration, string(migration)))

	const insert = `INSERT INTO schedule_posters (id, channel_id, x_user_id, post_url, image_url, posted_at, text_excerpt,
		review_status, week_start, week_end, translation_status, translated_image, translated_mime, translated_at)
		VALUES ($1, 'UCROQtXcp2loQEmvpe5rhJzQ', '1', 'https://x.com/i/status/1', 'https://pbs.twimg.com/media/a.jpg',
		now(), '', 'confirmed', '2026-11-09', '2026-11-15', $2, $3, $4, CASE WHEN $3::bytea IS NULL THEN NULL ELSE now() END)`

	jpeg := []byte{0xff, 0xd8, 0xff}

	for index, row := range []struct {
		status string
		image  []byte
		mime   any
	}{
		{status: "succeeded", image: jpeg, mime: "image/jpeg"},
		{status: "pending", image: jpeg, mime: "image/jpeg"},
		{status: "failed", image: jpeg, mime: "image/jpeg"},
		{status: "pending", image: nil, mime: nil},
	} {
		_, err := pool.Exec(ctx, insert, index+1, row.status, row.image, row.mime)
		require.NoError(t, err, "%s with image=%t", row.status, row.image != nil)
	}

	for index, row := range []struct {
		status     string
		image      []byte
		mime       any
		constraint string
	}{
		{status: "succeeded", image: nil, mime: nil, constraint: "chk_schedule_posters_translated_image"},
		{status: "not_requested", image: jpeg, mime: "image/jpeg", constraint: "chk_schedule_posters_translated_image"},
		{status: "pending", image: jpeg, mime: nil, constraint: "chk_schedule_posters_translated_image"},
		{status: "succeeded", image: jpeg, mime: "image/png", constraint: "chk_schedule_posters_translated_mime"},
	} {
		_, err := pool.Exec(ctx, insert, index+100, row.status, row.image, row.mime)
		require.ErrorContains(t, err, row.constraint, "%s with mime %v", row.status, row.mime)
	}
}
