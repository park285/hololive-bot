package dbtest

import (
	"io/fs"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	templateRichTextSummaryMigration = "359_richtext_collapsed_summary.sql"
	templateRichTextSummaryCustomKey = "CMD_LIVE_STREAMS"
	// 358 표준 richtext 본문을 그대로 가진 override의 키다.
	templateRichTextSummaryOverrideKey = "CMD_UPCOMING_STREAMS"
)

type templateRichTextSummarySeed struct{ standardSHA256, body string }

// 359는 358이 쓴 표준 richtext 기본값만 새 본문으로 바꾼다. 사용자 지정 richtext 기본값, plain 행과 채널 override는
// 본문이 같아도 그대로 두고, 다시 적용해도 아무것도 바꾸지 않는다.
func TestTemplateRichTextSummaryMigrationUpdatesStandardRichTextDefaultsOnly(t *testing.T) {
	t.Parallel()

	pool, dir := templateFormatUpgradePool(t)
	ctx := t.Context()

	for _, file := range templateFormatMigrations {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, file), file)
	}

	seeds := loadTemplateRichTextSummarySeeds(t, dir)
	formatSeeds := loadTemplateFormatSeeds(t, dir)

	require.Len(t, seeds, 5)

	for key, seed := range seeds {
		require.Equal(t, sha256Hex(formatSeeds[key].richTextBody), seed.standardSHA256, key)
		require.NotEqual(t, formatSeeds[key].richTextBody, seed.body, key)
	}

	_, err := pool.Exec(ctx, `UPDATE notification_templates SET body='사용자 지정 방송 목록'
WHERE template_key=$1 AND channel_id IS NULL AND format_variant='richtext'`, templateRichTextSummaryCustomKey)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant)
SELECT template_key,'summary-override',body,format_variant FROM notification_templates
WHERE template_key=$1 AND channel_id IS NULL AND format_variant='richtext'`, templateRichTextSummaryOverrideKey)
	require.NoError(t, err)

	const (
		richQuery     = `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NULL AND format_variant='richtext'`
		plainQuery    = `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NULL AND format_variant='plain'`
		overrideQuery = `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NOT NULL`
	)

	richBefore := templateFormatRows(t, pool, richQuery)
	plainBefore := templateFormatRows(t, pool, plainQuery)
	overridesBefore := templateFormatRows(t, pool, overrideQuery)
	protected := templateFormatProtectedSnapshot(t, pool)

	require.Len(t, overridesBefore, 1)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, templateRichTextSummaryMigration))

	richAfter := templateFormatRows(t, pool, richQuery)
	require.Len(t, richAfter, len(richBefore))

	for key, before := range richBefore {
		after := richAfter[key]
		seed, changed := seeds[key]

		if !changed || key == templateRichTextSummaryCustomKey {
			require.Equal(t, before, after, key)

			continue
		}

		require.Equal(t, before.id, after.id, key)
		require.Equal(t, before.version+1, after.version, key)
		require.Equal(t, seed.body, after.body, key)
	}

	require.Equal(t, plainBefore, templateFormatRows(t, pool, plainQuery))
	require.Equal(t, overridesBefore, templateFormatRows(t, pool, overrideQuery))
	require.Equal(t, protected, templateFormatProtectedSnapshot(t, pool))
	assertTemplateReplayStable(t, pool, dir, templateRichTextSummaryMigration)
}

func loadTemplateRichTextSummarySeeds(t *testing.T, dir string) map[string]templateRichTextSummarySeed {
	t.Helper()

	raw, err := fs.ReadFile(os.DirFS(dir), templateRichTextSummaryMigration)
	require.NoError(t, err)

	rows := regexp.MustCompile(`(?s)\('([A-Z0-9_]+)', '([0-9a-f]{64})', \$rich\$(.*?)\$rich\$\)`).FindAllStringSubmatch(string(raw), -1)
	seeds := make(map[string]templateRichTextSummarySeed, len(rows))

	for _, row := range rows {
		seeds[row[1]] = templateRichTextSummarySeed{standardSHA256: row[2], body: row[3]}
	}

	return seeds
}
