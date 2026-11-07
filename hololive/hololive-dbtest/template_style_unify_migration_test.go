package dbtest

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	templateStyleUnifyMigration = "370_template_style_unify.sql"
	templateStyleUnifyCustomKey = "CMD_HELP"
	// 표준 plain 본문을 그대로 가진 override의 키다.
	templateStyleUnifyOverrideKey = "CMD_LIVE_STREAMS"
)

var (
	templateStyleUnifySeedPattern = regexp.MustCompile(`(?s)\('([A-Z0-9_]+)', '(plain|richtext)', '([0-9a-f]{64})', \$body\$(.*?)\$body\$\)`)
	// 이전 migration의 dollar-quoted 문자열 전부다. 370이 바꾸는 직전 표준 본문은 이 중 하나와 해시가 같아야 한다.
	templateDollarLiteralPattern = regexp.MustCompile(`(?s)\$([a-z_]*)\$(.*?)\$([a-z_]*)\$`)
)

type templateStyleUnifySeed struct{ key, variant, standardSHA256, body string }

// 370은 직전 표준 기본값 24행만 통일 본문으로 바꾼다. 사용자 지정 기본값과 채널 override는 본문이 같아도 그대로 두고,
// 다시 적용해도 아무것도 바꾸지 않는다.
func TestTemplateStyleUnifyMigrationUpdatesStandardDefaultsOnly(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	seeds := loadTemplateStyleUnifySeeds(t, dir)
	restoreTemplateStyleUnifyStandardRows(t, pool, dir, seeds)

	changed := make(map[string]string, len(seeds))
	for _, seed := range seeds {
		changed[seed.key+"/"+seed.variant] = seed.body
	}

	assertTemplateMigrationUpdatesStandardDefaults(t, pool, dir, templateStyleUnifyMigration, changed, templateStyleUnifyCustomKey+"/plain")
}

// assertTemplateMigrationUpdatesStandardDefaults는 migration이 changed의 기본값만 새 본문으로 바꾸고(row_version +1),
// customKey 기본값·나머지 기본값·채널 override·발송 기록을 그대로 두며, 다시 적용해도 아무것도 바꾸지 않는지 확인한다.
func assertTemplateMigrationUpdatesStandardDefaults(
	t *testing.T, pool *pgxpool.Pool, dir, migration string, changed map[string]string, customKey string,
) {
	t.Helper()

	const (
		defaultQuery  = `SELECT template_key||'/'||format_variant,id,row_version,body FROM notification_templates WHERE channel_id IS NULL`
		overrideQuery = `SELECT template_key||'/'||format_variant,id,row_version,body FROM notification_templates WHERE channel_id IS NOT NULL`
	)

	before := templateFormatRows(t, pool, defaultQuery)
	overridesBefore := templateFormatRows(t, pool, overrideQuery)
	protected := templateFormatProtectedSnapshot(t, pool)

	require.Len(t, overridesBefore, 1)
	require.NoError(t, applyMigrationFile(t.Context(), pool, dir, migration))

	after := templateFormatRows(t, pool, defaultQuery)
	require.Len(t, after, len(before))

	for key, row := range before {
		body, ok := changed[key]
		if !ok || key == customKey {
			require.Equal(t, row, after[key], key)

			continue
		}

		require.Equal(t, row.id, after[key].id, key)
		require.Equal(t, row.version+1, after[key].version, key)
		require.Equal(t, body, after[key].body, key)
	}

	require.Equal(t, overridesBefore, templateFormatRows(t, pool, overrideQuery))
	require.Equal(t, protected, templateFormatProtectedSnapshot(t, pool))
	assertTemplateReplayStable(t, pool, dir, migration)
}

func loadTemplateStyleUnifySeeds(t *testing.T, dir string) []templateStyleUnifySeed {
	t.Helper()

	raw, err := fs.ReadFile(os.DirFS(dir), templateStyleUnifyMigration)
	require.NoError(t, err)

	rows := templateStyleUnifySeedPattern.FindAllStringSubmatch(string(raw), -1)
	seeds := make([]templateStyleUnifySeed, 0, len(rows))

	for _, row := range rows {
		seeds = append(seeds, templateStyleUnifySeed{key: row[1], variant: row[2], standardSHA256: row[3], body: row[4]})
	}

	require.Len(t, seeds, 24)

	return seeds
}

// previousTemplateLiterals는 370 이전 migration의 dollar-quoted 문자열을 sha256으로 찾는다.
func previousTemplateLiterals(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	literals := make(map[string]string)

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") || name >= templateStyleUnifyMigration {
			continue
		}

		raw, err := fs.ReadFile(os.DirFS(dir), name)
		require.NoError(t, err)

		for _, match := range templateDollarLiteralPattern.FindAllStringSubmatch(string(raw), -1) {
			if match[1] == match[3] {
				literals[sha256Hex(match[2])] = match[2]
			}
		}
	}

	return literals
}

// restoreTemplateStyleUnifyStandardRows는 전체 manifest 적용 DB를 370 이전 표준 본문으로 되돌리고, 사용자 지정 기본값 하나와
// 표준 본문을 그대로 가진 override 하나를 만든다.
func restoreTemplateStyleUnifyStandardRows(t *testing.T, pool *pgxpool.Pool, dir string, seeds []templateStyleUnifySeed) {
	t.Helper()

	ctx := t.Context()
	literals := previousTemplateLiterals(t, dir)

	const restore = `UPDATE notification_templates SET body=$3 WHERE template_key=$1 AND format_variant=$2 AND channel_id IS NULL`

	for _, seed := range seeds {
		old, ok := literals[seed.standardSHA256]
		require.True(t, ok, "이전 표준 본문 없음: "+seed.key+"/"+seed.variant)
		require.NotEqual(t, old, seed.body, seed.key+"/"+seed.variant)

		_, err := pool.Exec(ctx, restore, seed.key, seed.variant, old)
		require.NoError(t, err)
	}

	_, err := pool.Exec(ctx, restore, templateStyleUnifyCustomKey, "plain", "사용자 지정 도움말")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant)
SELECT template_key,'style-unify-override',body,format_variant FROM notification_templates
WHERE template_key=$1 AND channel_id IS NULL AND format_variant='plain'`, templateStyleUnifyOverrideKey)
	require.NoError(t, err)
}
