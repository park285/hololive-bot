package dbtest

import (
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const dropLLMFeatureStorageMigration = "009_drop_llm_feature_storage.sql"

func TestDropLLMFeatureStorageMigrationRewritesHelpSections(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	load := func(format string) string {
		var body string

		require.NoError(t, pool.QueryRow(ctx, `SELECT body FROM notification_templates
			WHERE template_key = 'CMD_HELP' AND channel_id IS NULL AND format_variant = $1`, format).Scan(&body))

		return body
	}

	require.Contains(t, load("plain"), "{{mdescape .Prefix}}알람 초기화\n\n[기념일]")
	require.Contains(t, load("richtext"), "{{mdescape .Prefix}}알람 초기화\n\n**기념일**")

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), dropLLMFeatureStorageMigration)
	require.NoError(t, err)

	const (
		customized  = "운영자 도움말"
		withSection = "운영자 머리말\n\n[뉴스]\n{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약\n{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태\n\n[행사]\n{{mdescape .Prefix}}행사 - 행사 알림 상태\n{{mdescape .Prefix}}행사 켜기 / 끄기\n\n운영자 꼬리말"
	)

	_, err = pool.Exec(ctx, `UPDATE notification_templates SET body = CASE format_variant WHEN 'plain' THEN $1 ELSE $2 END
		WHERE template_key = 'CMD_HELP' AND channel_id IS NULL`, withSection, customized)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, dropLLMFeatureStorageMigration, string(migration)))

	require.Equal(t, "운영자 머리말\n\n운영자 꼬리말", load("plain"))
	require.Equal(t, customized, load("richtext"))
}
