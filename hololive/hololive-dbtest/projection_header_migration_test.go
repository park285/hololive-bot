package dbtest

import (
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"
	"github.com/stretchr/testify/require"
)

const projectionHeaderMigration = "261_youtube_projection_header_eligibility.sql"

// projectionMigrationPoolBefore는 최종 schema를 역변경하지 않고 실제 manifest의 upgrade 출발점을 재현한다.
func projectionMigrationPoolBefore(t *testing.T, stop string) (*pgxpool.Pool, string) {
	t.Helper()

	pool := NewBlankPool(t)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	entries, err := dbmigrate.Manifest(os.DirFS(dir))
	require.NoError(t, err)
	require.NoError(t, ensureDBTestMigrationLedger(t.Context(), pool))

	for _, filename := range entries {
		if filename == stop {
			return pool, dir
		}

		require.NoError(t, applyManifestMigration(t.Context(), pool, dir, filename))
	}

	t.Fatalf("projection migration %s is not registered", stop)

	return nil, ""
}

func TestProjectionHeaderMigrationPreservesMembershipAndStorage(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, projectionHeaderMigration)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('RETIRED',1,repeat('a',64),now()-interval '8 days',now()-interval '9 days'),
		       ('CURRENT',1,repeat('b',64),now()+interval '1 hour',now());
		INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,
		    enabled,valid_until,member_since_generation,not_before)
		SELECT generation,'header-channel','community_page',40,120000,true,now()-interval '1 second',generation,now()+interval '1 hour'
		FROM youtube_collection_projection_generations;
		INSERT INTO youtube_collection_target_reasons(projection_generation,subject_key,observation_kind,reason_kind,reason_key)
		SELECT generation,'header-channel','community_page','operational_roster','header-channel'
		FROM youtube_collection_projection_generations;
	`)
	require.NoError(t, err)

	before := projectionUpgradeSnapshot(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, projectionHeaderMigration))
		require.Equal(t, before, projectionUpgradeSnapshot(t, pool), "cutover must not rewrite membership/reasons or expand function ACLs")
	}

	var removed, versioned, unknownRefreshClock int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='youtube_collection_targets' AND column_name='valid_until'`).Scan(&removed))
	require.Zero(t, removed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM youtube_collection_projection_generations WHERE eligibility_version=1`).Scan(&versioned))
	require.Equal(t, 2, versioned)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM youtube_collection_projection_generations WHERE validity_refreshed_at IS NULL`).Scan(&unknownRefreshClock))
	require.Equal(t, 2, unknownRefreshClock, "migration must not infer existing header refresh times or TTLs")

	// 런타임이 수립한 시각은 파일 재실행에서도 유지한다.
	refreshedAt := time.Now().UTC().Truncate(time.Microsecond)

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET validity_refreshed_at=$1 WHERE status='CURRENT'`, refreshedAt)
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, projectionHeaderMigration))

	var storedRefresh time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT validity_refreshed_at FROM youtube_collection_projection_generations WHERE status='CURRENT'`).Scan(&storedRefresh))
	require.True(t, storedRefresh.Equal(refreshedAt))
	require.Equal(t, before, projectionUpgradeSnapshot(t, pool))

	var valid bool

	query := `SELECT public.youtube_collection_membership_valid(ARRAY['community_page'],true,'header-channel',1,generation)
		FROM youtube_collection_projection_generations WHERE status='CURRENT'`
	require.NoError(t, pool.QueryRow(ctx, query).Scan(&valid))
	require.True(t, valid, "old target expiry and future admission time do not invalidate admitted membership")

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET valid_until=now()-interval '1 second' WHERE status='CURRENT'`)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, query).Scan(&valid))
	require.False(t, valid, "header expiry alone must revoke membership")

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET eligibility_version=0 WHERE status='CURRENT'`)
	require.ErrorContains(t, err, "chk_youtube_projection_eligibility_version")
}

// 시스템 tuple identity는 값이 같은 UPDATE나 table rewrite도 관측한다. 만료/version만 snapshot에서 제외한다.
func projectionUpgradeSnapshot(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var snapshot string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'targets', (SELECT jsonb_agg(jsonb_build_array(to_jsonb(t)-'valid_until',t.xmin::text,t.ctid::text)
		    ORDER BY projection_generation,subject_key,observation_kind) FROM youtube_collection_targets t),
		'reasons', (SELECT jsonb_agg(jsonb_build_array(to_jsonb(r),r.xmin::text,r.ctid::text)
		    ORDER BY projection_generation,subject_key,observation_kind,reason_kind,reason_key) FROM youtube_collection_target_reasons r),
		'headers', (SELECT jsonb_agg(to_jsonb(g)-'eligibility_version'-'validity_refreshed_at' ORDER BY generation) FROM youtube_collection_projection_generations g),
		'functions', (SELECT jsonb_agg(jsonb_build_array(proname,proowner,prosecdef,proacl) ORDER BY proname)
		    FROM pg_proc WHERE proname IN ('youtube_collection_membership_valid','lock_current_youtube_collection_projection',
		        'delete_retired_youtube_projection_batch','delete_retired_youtube_job_leases')),
		'relations', (SELECT jsonb_agg(jsonb_build_array(relname,relfilenode,relacl,reloptions) ORDER BY relname)
		    FROM pg_class WHERE oid IN ('youtube_collection_targets'::regclass,'youtube_collection_target_reasons'::regclass))
	)::text`).Scan(&snapshot))

	return snapshot
}
