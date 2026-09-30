package dbtest

import (
	"os"
	"testing"

	"github.com/park285/shared-go/v2/pkg/dbmigrate"
	"github.com/stretchr/testify/require"
)

const lifecycleOriginMigration = "245_youtube_live_lifecycle_origin.sql"

func TestLifecycleOriginMigrationConservativeBatchesAndReplay(t *testing.T) {
	pool := NewBlankPool(t)
	ctx := t.Context()
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	entries, err := dbmigrate.Manifest(os.DirFS(dir))
	require.NoError(t, err)
	require.NoError(t, ensureDBTestMigrationLedger(ctx, pool))

	for _, filename := range entries {
		if filename == lifecycleOriginMigration {
			break
		}

		require.NoError(t, applyManifestMigration(ctx, pool, dir, filename))
	}

	// 활성 projection 밖의 LIVE도 포함하고 여러 배치를 지나도록 2105행을 만든다.
	_, err = pool.Exec(ctx, `INSERT INTO youtube_live_sessions
        (video_id,channel_id,status,scheduled_start_time,last_seen_at)
        SELECT 'origin-'||lpad(n::text,4,'0'),'UC_origin',
               CASE WHEN n%2=0 THEN 'LIVE' ELSE 'UPCOMING' END,
               now()-interval '1 year',now()-interval '1 year'
        FROM generate_series(1,2105) n;
        INSERT INTO youtube_live_reconciliation_heads
        (video_id,status,last_live_positive_at,last_live_positive_seen_at)
        SELECT video_id,status,now()-interval '1 hour',now()-interval '1 hour'
        FROM youtube_live_sessions WHERE status='LIVE';
        INSERT INTO youtube_live_reconciliation_heads (video_id,status)
        VALUES ('origin-0001','UPCOMING');
        INSERT INTO youtube_live_reconciliation_heads (video_id,status,last_upcoming_positive_at)
        VALUES ('origin-0003','UPCOMING',now()-interval '1 year');
        INSERT INTO source_observation_applications
        (provider,observation_kind,subject_key,evidence_sha256,entity_kind,entity_key,decision,effective_at)
        VALUES ('youtubejs','live_snapshot','UC_origin',repeat('a',64),'youtube_live_session','origin-0005','APPLIED',now()),
               ('youtubejs','video_list','UC_origin',repeat('a',64),'youtube_live_session','origin-0007','APPLIED',now()),
               ('youtubejs','live_snapshot','UC_origin',repeat('a',64),'youtube_live_session','origin-0009','LIVE_START_UNCONFIRMED',now()),
               ('youtubejs','video_live_check','origin-0011',repeat('a',64),'youtube_live_session','origin-0011','ENDED',now())`)
	require.NoError(t, err)

	var before string

	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(session) ORDER BY video_id)::text
        FROM youtube_live_sessions session`).Scan(&before))
	require.NoError(t, applyMigrationFile(ctx, pool, dir, lifecycleOriginMigration))

	var observed, unknown, metadata int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE lifecycle_origin='observed'),
        count(*) FILTER(WHERE lifecycle_origin='legacy_unknown'),count(*) FILTER(WHERE lifecycle_origin='metadata_only')
        FROM youtube_live_sessions`).Scan(&observed, &unknown, &metadata))
	require.Equal(t, 1054, observed)
	require.Equal(t, 1051, unknown)
	require.Zero(t, metadata, "head absence must not prove metadata-only origin")

	var after string

	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(session)-'lifecycle_origin' ORDER BY video_id)::text
        FROM youtube_live_sessions session`).Scan(&after))
	require.Equal(t, before, after, "classification must preserve all status and clock values")

	var rowVersions string

	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(video_id,xmin::text) ORDER BY video_id)::text
        FROM youtube_live_sessions`).Scan(&rowVersions))
	require.NoError(t, applyMigrationFile(ctx, pool, dir, lifecycleOriginMigration))

	var replayVersions string

	require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(video_id,xmin::text) ORDER BY video_id)::text
        FROM youtube_live_sessions`).Scan(&replayVersions))
	require.Equal(t, rowVersions, replayVersions, "replay must not rewrite unchanged origins")

	_, err = pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,lifecycle_origin)
        VALUES ('invalid-origin','UC_origin','UPCOMING','guessed')`)
	requireSQLState(t, err, sqlStateCheckViolation)

	var procedureGone bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regprocedure('public.backfill_youtube_live_lifecycle_origin()') IS NULL`).Scan(&procedureGone))
	require.True(t, procedureGone, "migration must not leave a runtime backfill actuator")
}
