package dbtest

import (
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"
	"github.com/stretchr/testify/require"
)

const richTextTemplatesMigration = "347_richtext_templates.sql"

func TestRichTextTemplatesMigrationPreservesCustomBodiesAndFrozenRequests(t *testing.T) {
	t.Parallel()

	pool, dir := richTextTemplatesUpgradePool(t)
	bodies := loadTemplateMigrationBodies(t, richTextTemplatesMigration)
	ctx := t.Context()

	// 새 migration의 old_body를 DB에 주입하지 않고 실제 manifest가 만든 전체 기준선과 대조한다.
	rows, err := pool.Query(ctx, `SELECT template_key, body, id, row_version FROM notification_templates WHERE channel_id IS NULL`)
	require.NoError(t, err)

	type identity struct{ id, version int64 }
	before := make(map[string]identity)
	for rows.Next() {
		var key, body string
		var value identity

		require.NoError(t, rows.Scan(&key, &body, &value.id, &value.version))
		pair, exists := bodies[key]
		require.True(t, exists, "기준선 키 누락: %s", key)
		require.Equal(t, pair.oldBody, body, "최종 기준선 불일치: %s", key)
		before[key] = value
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Len(t, before, 42)
	require.Len(t, bodies, len(before))

	// 표준 본문과 바이트가 같은 override도 전환 대상이 아니다.
	_, err = pool.Exec(ctx, `INSERT INTO notification_templates(template_key,channel_id,body)
SELECT template_key,'richtext-override',body FROM notification_templates WHERE channel_id IS NULL;
INSERT INTO notification_template_revisions(template_id,body)
SELECT id,body FROM notification_templates WHERE channel_id IS NULL;
INSERT INTO alarm_dispatch_send_units(unit_key,dispatch_group_key,room_id,client_request_id,
request_body,request_route,request_body_hash,request_delivery_ids,base_client_request_id,request_generation)
VALUES(repeat('a',64),'template-upgrade','legacy-alarm','legacy:alarm:r1',
'**저장된 본문**','text',repeat('b',64),ARRAY[1]::bigint[],'legacy:alarm',1);
INSERT INTO youtube_notification_send_request(base_id,room_id,message,message_hash,route,dedupe_keys,member_ids,generation)
VALUES('legacy:youtube','legacy-youtube','**저장된 본문**',repeat('c',64),'markdown',ARRAY['legacy-key'],ARRAY[1]::bigint[],2);
INSERT INTO bot_reply_outbox(message_id,phase,ordinal,room_id,payload,payload_hash,client_request_id)
VALUES('legacy-bot','reply',0,'legacy-room','{"body":"**저장된 본문**","route":"text","threadId":"legacy-thread"}'::jsonb,repeat('d',64),'legacy:bot')`)
	require.NoError(t, err)

	protected := richTextProtectedSnapshot(t, pool)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, richTextTemplatesMigration))

	for key, previous := range before {
		var body string
		var current identity

		require.NoError(t, pool.QueryRow(ctx, `SELECT body,id,row_version FROM notification_templates WHERE template_key=$1 AND channel_id IS NULL`, key).
			Scan(&body, &current.id, &current.version))
		require.Equal(t, bodies[key].newBody, body, key)
		require.Equal(t, previous.id, current.id, key)
		require.Equal(t, previous.version+1, current.version, key)
	}
	require.Equal(t, protected, richTextProtectedSnapshot(t, pool))
	assertTemplateReplayStable(t, pool, dir, richTextTemplatesMigration)

	// 모든 키의 사용자 지정 기본값도 동일한 본문·버전·갱신 시각을 유지한다.
	_, err = pool.Exec(ctx, `UPDATE notification_templates SET body='사용자 지정: ' || body WHERE channel_id IS NULL`)
	require.NoError(t, err)
	assertTemplateReplayStable(t, pool, dir, richTextTemplatesMigration)
	require.Equal(t, protected, richTextProtectedSnapshot(t, pool))
}

func richTextTemplatesUpgradePool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()

	pool := NewBlankPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)
	entries, err := dbmigrate.Manifest(os.DirFS(dir))
	require.NoError(t, err)
	require.NoError(t, ensureDBTestMigrationLedger(t.Context(), pool))

	for _, filename := range entries {
		if filename == richTextTemplatesMigration {
			return pool, dir
		}
		require.NoError(t, applyManifestMigration(t.Context(), pool, dir, filename))
	}

	t.Fatal("RichText 템플릿 migration이 manifest에 없습니다")
	return nil, ""
}

func richTextProtectedSnapshot(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var snapshot string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_array(
(SELECT jsonb_agg(to_jsonb(t) || jsonb_build_object('xmin',xmin::text) ORDER BY id) FROM notification_templates t WHERE channel_id IS NOT NULL),
(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM notification_template_revisions r),
(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM alarm_dispatch_send_units u),
(SELECT jsonb_agg(to_jsonb(r) ORDER BY base_id) FROM youtube_notification_send_request r),
(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM bot_reply_outbox o)
)::text`).Scan(&snapshot))
	return snapshot
}
