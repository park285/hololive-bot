package dbtest

import (
	"io/fs"
	"os"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

var streamListCollabSeedPattern = regexp.MustCompile(`(?s)\('([A-Z0-9_]+)', '(plain|richtext)', '([0-9a-f]{64})', \$body\$(.*?)\$body\$\)`)

const (
	streamListCollabMigration = "369_stream_list_collab_members.sql"
	streamListCollabCustomKey = "CMD_LIVE_STREAMS"
	// 표준 richtext 본문을 그대로 가진 override의 키다.
	streamListCollabOverrideKey = "CMD_UPCOMING_STREAMS"
)

type streamListCollabSeed struct{ key, variant, standardSHA256, body string }

// 369는 358 plain·359 richtext 표준 기본값만 콜라보 줄 본문으로 바꾼다. 사용자 지정 기본값과 채널 override는
// 본문이 같아도 그대로 두고, 다시 적용해도 아무것도 바꾸지 않는다.
func TestStreamListCollabMembersMigrationUpdatesStandardDefaultsOnly(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	seeds := loadStreamListCollabSeeds(t, dir)
	restoreStreamListCollabStandardRows(t, pool, dir, seeds)

	const (
		defaultQuery  = `SELECT template_key||'/'||format_variant,id,row_version,body FROM notification_templates WHERE channel_id IS NULL`
		overrideQuery = `SELECT template_key||'/'||format_variant,id,row_version,body FROM notification_templates WHERE channel_id IS NOT NULL`
	)

	before := templateFormatRows(t, pool, defaultQuery)
	overridesBefore := templateFormatRows(t, pool, overrideQuery)
	protected := templateFormatProtectedSnapshot(t, pool)

	require.Len(t, overridesBefore, 1)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, streamListCollabMigration))

	after := templateFormatRows(t, pool, defaultQuery)
	require.Len(t, after, len(before))

	changed := make(map[string]string, len(seeds))
	for _, seed := range seeds {
		changed[seed.key+"/"+seed.variant] = seed.body
	}

	for key, row := range before {
		body, ok := changed[key]
		if !ok || key == streamListCollabCustomKey+"/plain" {
			require.Equal(t, row, after[key], key)

			continue
		}

		require.Equal(t, row.id, after[key].id, key)
		require.Equal(t, row.version+1, after[key].version, key)
		require.Equal(t, body, after[key].body, key)
	}

	require.Equal(t, overridesBefore, templateFormatRows(t, pool, overrideQuery))
	require.Equal(t, protected, templateFormatProtectedSnapshot(t, pool))
	assertTemplateReplayStable(t, pool, dir, streamListCollabMigration)
}

func loadStreamListCollabSeeds(t *testing.T, dir string) []streamListCollabSeed {
	t.Helper()

	raw, err := fs.ReadFile(os.DirFS(dir), streamListCollabMigration)
	require.NoError(t, err)

	rows := streamListCollabSeedPattern.FindAllStringSubmatch(string(raw), -1)
	seeds := make([]streamListCollabSeed, 0, len(rows))

	for _, row := range rows {
		seeds = append(seeds, streamListCollabSeed{key: row[1], variant: row[2], standardSHA256: row[3], body: row[4]})
	}

	require.Len(t, seeds, 6)

	return seeds
}

// restoreStreamListCollabStandardRows는 전체 manifest 적용 DB를 369 이전 표준 본문으로 되돌리고, 사용자 지정 기본값 하나와
// 표준 본문을 그대로 가진 override 하나를 만든다.
func restoreStreamListCollabStandardRows(t *testing.T, pool *pgxpool.Pool, dir string, seeds []streamListCollabSeed) {
	t.Helper()

	ctx := t.Context()
	plainSeeds := loadTemplateFormatSeeds(t, dir)
	richSeeds := loadTemplateRichTextSummarySeeds(t, dir)
	standard := func(seed streamListCollabSeed) string {
		if seed.variant == "plain" {
			return plainSeeds[seed.key].plainBody
		}

		return richSeeds[seed.key].body
	}

	const restore = `UPDATE notification_templates SET body=$3 WHERE template_key=$1 AND format_variant=$2 AND channel_id IS NULL`

	for _, seed := range seeds {
		old := standard(seed)
		require.NotEmpty(t, old, seed.key+"/"+seed.variant)
		require.Equal(t, sha256Hex(old), seed.standardSHA256, seed.key+"/"+seed.variant)
		require.Contains(t, seed.body, "콜라보: {{mdsafe (displayline .CollabMembers)}}", seed.key+"/"+seed.variant)

		_, err := pool.Exec(ctx, restore, seed.key, seed.variant, old)
		require.NoError(t, err)
	}

	_, err := pool.Exec(ctx, restore, streamListCollabCustomKey, "plain", "사용자 지정 방송 목록")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant)
SELECT template_key,'collab-override',body,format_variant FROM notification_templates
WHERE template_key=$1 AND channel_id IS NULL AND format_variant='richtext'`, streamListCollabOverrideKey)
	require.NoError(t, err)
}
