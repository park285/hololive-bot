package dbtest

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	helpSchedulePosterMigration = "011_help_schedule_poster.sql"
	helpSchedulePosterLine      = "{{mdescape .Prefix}}스케쥴 [멤버명] - 이번 주 일정 포스터\n"
)

func TestHelpSchedulePosterMigrationUpdatesStandardDefaultsOnly(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	load := func(format, channel string) string {
		var body string

		require.NoError(t, pool.QueryRow(ctx, `SELECT body FROM notification_templates
			WHERE template_key = 'CMD_HELP' AND format_variant = $1 AND channel_id IS NOT DISTINCT FROM NULLIF($2, '')`,
			format, channel).Scan(&body))

		return body
	}

	migrated := map[string]string{}

	for _, format := range []string{"plain", "richtext"} {
		migrated[format] = load(format, "")
		require.Equal(t, 1, strings.Count(migrated[format], "방송 시간 합계(기본 7일)\n"+helpSchedulePosterLine), format)
	}

	standard := strings.Replace(migrated["richtext"], helpSchedulePosterLine, "", 1)

	const restore = `UPDATE notification_templates SET body = $2 WHERE template_key = 'CMD_HELP' AND format_variant = $1 AND channel_id IS NULL`

	_, err := pool.Exec(ctx, restore, "richtext", standard)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, restore, "plain", "사용자 지정 도움말")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key, channel_id, body, format_variant)
		VALUES ('CMD_HELP', 'help-schedule-poster-override', $1, 'richtext')`, standard)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), helpSchedulePosterMigration)
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, applyMigrationContent(ctx, pool, helpSchedulePosterMigration, string(migration)))
	}

	require.Equal(t, migrated["richtext"], load("richtext", ""))
	require.Equal(t, "사용자 지정 도움말", load("plain", ""))
	require.Equal(t, standard, load("richtext", "help-schedule-poster-override"))
}
