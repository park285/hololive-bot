package dbtest

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPayloadPrivilegesOverrideBroadDefaultACL(t *testing.T) {
	pool, dir := channelStatisticsRemovalPool(t)
	ctx := t.Context()
	roles := createObservationGrantRoles(t, pool)
	replacer := strings.NewReplacer("hololive_runtime", roles.runtime, "hololive_scraper", roles.scraper)
	prepareBroadPayloadACL(t, pool, dir, replacer, roles)

	_, err := pool.Exec(ctx, "CREATE TABLE unrelated_payload_acl_probe (id bigint)")
	require.NoError(t, err)

	raw, err := fs.ReadFile(os.DirFS(dir), "255_payload_dictionary_least_privilege.sql")
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, applyMigrationContent(ctx, pool, "255_payload_dictionary_least_privilege.sql", replacer.Replace(string(raw))))
	}

	for _, role := range []string{roles.runtime, roles.scraper} {
		var unrelatedInsert bool

		require.NoError(t, pool.QueryRow(ctx,
			"SELECT has_table_privilege($1, 'unrelated_payload_acl_probe', 'INSERT')", role).Scan(&unrelatedInsert))
		require.True(t, unrelatedInsert, "다른 테이블과 기본 ACL은 변경하지 않습니다")
	}

	assertPayloadDirectWritesDenied(t, pool, roles)
	assertPayloadPublicationAndGCAuthorized(t, pool, roles)
}

func prepareBroadPayloadACL(t *testing.T, pool *pgxpool.Pool, dir string, replacer *strings.Replacer, roles observationGrantRoles) {
	t.Helper()

	ctx := t.Context()

	// 운영과 같은 기본 ACL을 먼저 설정해야 CREATE TABLE이 상속하는 과도한 권한을 재현합니다.
	for _, role := range []string{roles.runtime, roles.scraper} {
		quoted := pgx.Identifier{role}.Sanitize()
		_, err := pool.Exec(ctx, "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO "+quoted)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO "+quoted)
		require.NoError(t, err)
	}

	for _, filename := range []string{
		"238_source_observation_payload_prepare.sql", "239_source_observation_payload_index.sql",
		"240_source_observation_payload_backfill_index.sql", "241_source_observation_payload_cutover.sql",
	} {
		raw, err := fs.ReadFile(os.DirFS(dir), filename)
		require.NoError(t, err)
		require.NoError(t, applyMigrationContent(ctx, pool, filename, replacer.Replace(string(raw))))
	}

	var inheritedDelete bool

	require.NoError(t, pool.QueryRow(ctx,
		"SELECT has_table_privilege($1, 'source_observation_payloads', 'DELETE')", roles.runtime).Scan(&inheritedDelete))
	require.True(t, inheritedDelete, "기본 ACL에 의한 실제 운영 권한 확대를 재현해야 합니다")
}

func assertPayloadDirectWritesDenied(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	for _, test := range []struct {
		name string
		role string
		sql  string
	}{
		{"runtime_insert", roles.runtime, `INSERT INTO source_observation_payloads (observation_kind,schema_version,canonical_profile,payload_sha256,payload) SELECT 'community_page',1,'source-observation-canonical-json-v1',sha256('{}'::bytea),'{}'::jsonb WHERE false`},
		{"runtime_update", roles.runtime, "UPDATE source_observation_payloads SET payload=payload WHERE false"},
		{"runtime_delete", roles.runtime, "DELETE FROM source_observation_payloads WHERE false"},
		{"runtime_gc_state_update", roles.runtime, "UPDATE source_observation_payload_gc_state SET cursor_id=0 WHERE false"},
		{"runtime_sequence", roles.runtime, "SELECT nextval('source_observation_payloads_id_seq')"},
		{"scraper_update", roles.scraper, "UPDATE source_observation_payloads SET payload=payload WHERE false"},
		{"scraper_delete", roles.scraper, "DELETE FROM source_observation_payloads WHERE false"},
		{"scraper_gc_state_read", roles.scraper, "SELECT count(*) FROM source_observation_payload_gc_state"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertPayloadPermissionDenied(t, pool, test.role, test.sql)
		})
	}
}

func assertPayloadPublicationAndGCAuthorized(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	ctx := t.Context()

	// 정상 발행과 회수는 제한 후에도 실제 역할로 실행되어야 합니다.
	tx := payloadRoleTransaction(t, pool, roles.scraper)

	_, err := tx.Exec(ctx, `INSERT INTO source_observation_payloads
        (observation_kind,schema_version,canonical_profile,payload_sha256,payload,created_at)
        VALUES ('community_page',1,'source-observation-canonical-json-v1',sha256('{}'::bytea),'{}',now()-interval '2 hours')`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	tx = payloadRoleTransaction(t, pool, roles.runtime)

	var removed int64

	require.NoError(t, tx.QueryRow(ctx, "SELECT delete_source_observation_payload_batch(now()-interval '1 hour',1000)").Scan(&removed))
	require.EqualValues(t, 1, removed)
	require.NoError(t, tx.Commit(ctx))
}

func assertPayloadPermissionDenied(t *testing.T, pool *pgxpool.Pool, role, sql string) {
	t.Helper()

	ctx := t.Context()
	tx := payloadRoleTransaction(t, pool, role)
	_, err := tx.Exec(ctx, sql)
	require.Error(t, err)

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok)
	require.Equal(t, "42501", pgErr.Code)
	require.NoError(t, tx.Rollback(ctx))
}

func payloadRoleTransaction(t *testing.T, pool *pgxpool.Pool, role string) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer cancel()

		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback payload role transaction: %v", rollbackErr)
		}
	})

	_, err = tx.Exec(t.Context(), "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize())
	require.NoError(t, err)

	return tx
}
