package dbtest

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"
	"github.com/stretchr/testify/require"
)

var templateFormatSeedPattern = regexp.MustCompile(`(?s)\('([A-Z0-9_]+)', '([0-9a-f]{64})', \$plain\$(.*?)\$plain\$, \$rich\$(.*?)\$rich\$\)`)

const (
	templateFormatColumnMigration = "353_template_format_variant.sql"
	templateFormatDataMigration   = "358_template_format_variants.sql"
	templateFormatCustomKey       = "CMD_HELP"
	templateFormatOverrideChannel = "format-override"
	// 347 표준 본문을 그대로 가진 override의 키다.
	templateFormatStandardOverrideKey = "CMD_LIVE_STREAMS"
)

var templateFormatMigrations = []string{
	templateFormatColumnMigration,
	"354_template_default_format_index.sql",
	"355_template_channel_format_index.sql",
	"356_drop_template_default_index.sql",
	"357_drop_template_channel_index.sql",
	templateFormatDataMigration,
}

type templateFormatSeed struct{ standardSHA256, plainBody, richTextBody string }

type templateFormatRow struct {
	id, version int64
	body        string
}

// 표준 기본값만 347 이전 일반 텍스트와 새 RichText 본문으로 나누고, 사용자 지정 본문과 override는 두 경로가 지금과
// 같은 본문을 받도록 복사한다.
func TestTemplateFormatVariantsMigrationSplitsStandardBodiesAndPreservesCustomRows(t *testing.T) {
	t.Parallel()

	pool, dir := templateFormatUpgradePool(t)
	ctx := t.Context()
	seeds := loadTemplateFormatSeeds(t, dir)
	richTextPairs := loadTemplateMigrationBodies(t, richTextTemplatesMigration)

	require.Len(t, seeds, 42)

	for key, seed := range seeds {
		pair, exists := richTextPairs[key]
		require.True(t, exists, key)
		// 표준 판정 해시는 347이 쓴 본문이고, plain 복원 본문은 347 이전 본문이다.
		require.Equal(t, sha256Hex(pair.newBody), seed.standardSHA256, key)
		require.Equal(t, pair.oldBody, seed.plainBody, key)
	}

	seedCustomTemplateRows(t, pool)

	defaults := templateFormatRows(t, pool, `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NULL`)
	overrides := templateFormatRows(t, pool, `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NOT NULL`)
	protected := templateFormatProtectedSnapshot(t, pool)

	for _, file := range templateFormatMigrations {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, file), file)
	}

	plainDefaults := templateFormatRows(t, pool, `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NULL AND format_variant='plain'`)
	richDefaults := templateFormatRows(t, pool, `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NULL AND format_variant='richtext'`)
	require.Len(t, plainDefaults, len(defaults))
	require.Len(t, richDefaults, len(defaults))

	for key, before := range defaults {
		plain := plainDefaults[key]
		require.Equal(t, before.id, plain.id, key)

		if key == templateFormatCustomKey {
			require.Equal(t, before, plain, key)
			require.Equal(t, before.body, richDefaults[key].body, key)

			continue
		}

		require.Equal(t, seeds[key].plainBody, plain.body, key)
		require.Equal(t, before.version+1, plain.version, key)
		require.Equal(t, seeds[key].richTextBody, richDefaults[key].body, key)
	}

	assertTemplateFormatOverridesCopied(t, pool, overrides)
	require.Equal(t, protected, templateFormatProtectedSnapshot(t, pool))
	assertTemplateReplayStable(t, pool, dir, templateFormatDataMigration)
	assertTemplateFormatConstraints(t, pool)
}

// seedCustomTemplateRows는 사용자 지정 기본값 하나와 모든 키의 채널 override를 만든다. 그중 하나는 347 표준
// 본문과 같아서, 표준 판정이 기본값에만 적용되는지 확인한다.
func seedCustomTemplateRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body='사용자 지정 도움말' WHERE template_key=$1 AND channel_id IS NULL`, templateFormatCustomKey)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `INSERT INTO notification_templates(template_key,channel_id,body)
SELECT template_key,$1,CASE WHEN template_key=$2 THEN body ELSE '채널 지정: ' || body END
FROM notification_templates WHERE channel_id IS NULL`, templateFormatOverrideChannel, templateFormatStandardOverrideKey)
	require.NoError(t, err)
}

// 채널 override의 plain 행은 그대로 두고 같은 본문을 richtext 행으로 복사한다.
func assertTemplateFormatOverridesCopied(t *testing.T, pool *pgxpool.Pool, overrides map[string]templateFormatRow) {
	t.Helper()

	plainOverrides := templateFormatRows(t, pool, `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NOT NULL AND format_variant='plain'`)
	richOverrides := templateFormatRows(t, pool, `SELECT template_key,id,row_version,body FROM notification_templates WHERE channel_id IS NOT NULL AND format_variant='richtext'`)
	require.Equal(t, overrides, plainOverrides)
	require.Len(t, richOverrides, len(overrides))

	for key, before := range overrides {
		require.Equal(t, before.body, richOverrides[key].body, key)
	}
}

// 같은 변형의 기본값 중복과 정의되지 않은 변형은 저장 단계에서 거절하고, 변형만 바뀌어도 렌더러 캐시가 새 행으로
// 보도록 row_version을 올린다.
func assertTemplateFormatConstraints(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()

	_, err := pool.Exec(ctx, `INSERT INTO notification_templates(template_key,body,format_variant) VALUES($1,'중복','richtext')`, templateFormatCustomKey)
	require.ErrorContains(t, err, "ux_notification_templates_default_format")

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant) VALUES($1,$2,'중복','plain')`,
		templateFormatCustomKey, templateFormatOverrideChannel)
	require.ErrorContains(t, err, "ux_notification_templates_channel_format")

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body,format_variant) VALUES($1,'other','본문','markdown')`, templateFormatCustomKey)
	require.ErrorContains(t, err, "notification_templates_format_variant_check")

	var version int64

	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body) VALUES($1,'other-format','본문')`, templateFormatCustomKey)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `UPDATE notification_templates SET format_variant='richtext'
WHERE template_key=$1 AND channel_id='other-format' RETURNING row_version`, templateFormatCustomKey).Scan(&version))
	require.Equal(t, int64(2), version)
}

func templateFormatUpgradePool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()

	pool := NewBlankPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	entries, err := dbmigrate.Manifest(os.DirFS(dir))
	require.NoError(t, err)
	require.NoError(t, ensureDBTestMigrationLedger(t.Context(), pool))

	for _, filename := range entries {
		if filename == templateFormatColumnMigration {
			return pool, dir
		}

		require.NoError(t, applyManifestMigration(t.Context(), pool, dir, filename))
	}

	t.Fatal("템플릿 형식 변형 migration이 manifest에 없습니다")

	return nil, ""
}

func loadTemplateFormatSeeds(t *testing.T, dir string) map[string]templateFormatSeed {
	t.Helper()

	raw, err := fs.ReadFile(os.DirFS(dir), templateFormatDataMigration)
	require.NoError(t, err)

	rows := templateFormatSeedPattern.FindAllStringSubmatch(string(raw), -1)
	seeds := make(map[string]templateFormatSeed, len(rows))

	for _, row := range rows {
		seeds[row[1]] = templateFormatSeed{standardSHA256: row[2], plainBody: row[3], richTextBody: row[4]}
	}

	return seeds
}

func templateFormatRows(t *testing.T, pool *pgxpool.Pool, query string) map[string]templateFormatRow {
	t.Helper()

	rows, err := pool.Query(t.Context(), query)
	require.NoError(t, err)

	defer rows.Close()

	result := make(map[string]templateFormatRow)

	for rows.Next() {
		var (
			key string
			row templateFormatRow
		)

		require.NoError(t, rows.Scan(&key, &row.id, &row.version, &row.body))
		require.NotContains(t, result, key)

		result[key] = row
	}

	require.NoError(t, rows.Err())

	return result
}

func templateFormatProtectedSnapshot(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var snapshot string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_array(
(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM notification_template_revisions r),
(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM alarm_dispatch_send_units u),
(SELECT jsonb_agg(to_jsonb(r) ORDER BY base_id) FROM youtube_notification_send_request r),
(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM bot_reply_outbox o)
)::text`).Scan(&snapshot))

	return snapshot
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}
