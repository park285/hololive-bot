package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const orphanedLegacyLiveMigration = "324_youtube_live_orphaned_legacy_live_end.sql"

func TestOrphanedLegacyLiveMigrationEndsOnlyUnobservableSessions(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO members(slug,channel_id,english_name,org,sync_source,aliases,status,is_graduated) VALUES
        ('orphan-active','UC_active','Active','Hololive','manual','{"ko":[],"ja":[]}','active',false),
        ('orphan-graduated','UC_graduated','Graduated','Hololive','manual','{"ko":[],"ja":[]}','graduated',true);
        INSERT INTO youtube_live_sessions
        (video_id,channel_id,status,started_at,live_first_seen_at,last_seen_at,lifecycle_origin) VALUES
        ('orphan-gone','UC_gone','LIVE',now()-interval '140 days',now()-interval '140 days',now()-interval '139 days','legacy_unknown'),
        ('orphan-grad','UC_graduated','LIVE',now()-interval '140 days',now()-interval '140 days',now()-interval '138 days','legacy_unknown'),
        ('orphan-roster','UC_active','LIVE',now()-interval '140 days',now()-interval '140 days',now()-interval '137 days','legacy_unknown'),
        ('orphan-head','UC_gone','LIVE',now()-interval '2 hours',now()-interval '2 hours',now()-interval '1 hour','legacy_unknown'),
        ('orphan-observed','UC_gone','LIVE',now()-interval '2 hours',now()-interval '2 hours',now()-interval '1 hour','observed'),
        ('orphan-upcoming','UC_gone','UPCOMING',NULL,NULL,now()-interval '139 days','legacy_unknown');
        INSERT INTO youtube_live_reconciliation_heads (video_id,status,last_live_positive_at,last_live_positive_seen_at)
        VALUES ('orphan-head','LIVE',now()-interval '1 hour',now()-interval '1 hour')`)
	if err != nil {
		t.Fatal(err)
	}

	if err = applyMigrationFile(ctx, pool, dir, orphanedLegacyLiveMigration); err != nil {
		t.Fatal(err)
	}

	ended := endedOrphanSessions(t, pool)
	if len(ended) != 2 || !ended["orphan-gone"] || !ended["orphan-grad"] {
		t.Fatalf("ended sessions = %v; want only legacy headless LIVE outside the operational roster", ended)
	}

	versions := liveRowVersions(t, pool)
	if err = applyMigrationFile(ctx, pool, dir, orphanedLegacyLiveMigration); err != nil {
		t.Fatal(err)
	}

	if liveRowVersions(t, pool) != versions {
		t.Fatal("replaying the orphaned LIVE cleanup rewrote rows")
	}
}

// endedOrphanSessions는 ENDED 세션 집합을 돌려주고, 정리된 행이 legacy 출처·마지막 관측 종료 시각·종료 관측
// 시각을 갖고 runtime state에서 빠졌는지 확인한다.
func endedOrphanSessions(t *testing.T, pool *pgxpool.Pool) map[string]bool {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT video_id,lifecycle_origin,
            ended_at IS NOT DISTINCT FROM last_seen_at AND status_observed_at IS NOT NULL,
            EXISTS (SELECT 1 FROM youtube_live_runtime_state state WHERE state.video_id=session.video_id)
        FROM youtube_live_sessions session WHERE status='ENDED' ORDER BY video_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	ended := map[string]bool{}

	for rows.Next() {
		var (
			videoID, origin  string
			stamped, runtime bool
		)

		if scanErr := rows.Scan(&videoID, &origin, &stamped, &runtime); scanErr != nil {
			t.Fatal(scanErr)
		}

		if origin != "legacy_unknown" || !stamped || runtime {
			t.Fatalf("%s ended with origin=%s stamped=%v runtime=%v", videoID, origin, stamped, runtime)
		}

		ended[videoID] = true
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	return ended
}
