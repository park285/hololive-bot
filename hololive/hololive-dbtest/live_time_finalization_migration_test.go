package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const timeFinalizationMigration = "323_youtube_live_time_finalization.sql"

func TestTimeFinalizationMigrationGuardsProgressShapeAndReplays(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions (video_id,channel_id,status,started_at,ended_at,lifecycle_origin)
        SELECT 'tf-'||n,'UC_tf','ENDED',now()-interval '3 hours',now()-interval '1 hour','observed' FROM generate_series(1,4) n`); err != nil {
		t.Fatal(err)
	}

	var source string

	if err := pool.QueryRow(ctx, `SELECT times_source FROM youtube_live_sessions WHERE video_id='tf-1'`).Scan(&source); err != nil || source != "observed" {
		t.Fatalf("new session times_source = %q, %v; want observed", source, err)
	}

	// 아직 결과가 없는 backlog만 다음 시도 시각 없이 pending일 수 있고, 최종 상태는 완료 시각을 가진다.
	if _, err := pool.Exec(ctx, `INSERT INTO youtube_live_time_finalizations
        (video_id,queue,outcome,reason,attempts,next_attempt_at,completed_at) VALUES
        ('tf-1','backlog','pending',NULL,0,NULL,NULL),
        ('tf-2','new','pending','identity_missing',1,now(),NULL),
        ('tf-3','new','finalized',NULL,0,NULL,now()),
        ('tf-4','backlog','exhausted','identity_missing',4,NULL,now())`); err != nil {
		t.Fatalf("valid progress rows rejected: %v", err)
	}

	for name, statement := range map[string]string{
		"unknown times source":        `UPDATE youtube_live_sessions SET times_source='guessed' WHERE video_id='tf-1'`,
		"unscheduled new pending":     `UPDATE youtube_live_time_finalizations SET next_attempt_at=NULL WHERE video_id='tf-2'`,
		"retried backlog unscheduled": `UPDATE youtube_live_time_finalizations SET attempts=1 WHERE video_id='tf-1'`,
		"pending with completion":     `UPDATE youtube_live_time_finalizations SET completed_at=now() WHERE video_id='tf-2'`,
		"finalized with reason":       `UPDATE youtube_live_time_finalizations SET reason='start_mismatch' WHERE video_id='tf-3'`,
		"exhausted without reason":    `UPDATE youtube_live_time_finalizations SET reason=NULL WHERE video_id='tf-4'`,
		"attempts beyond contract":    `UPDATE youtube_live_time_finalizations SET attempts=5 WHERE video_id='tf-4'`,
		"deferrals beyond contract":   `UPDATE youtube_live_time_finalizations SET deferrals=13 WHERE video_id='tf-4'`,
		"unknown outcome":             `UPDATE youtube_live_time_finalizations SET outcome='skipped' WHERE video_id='tf-3'`,
		"unknown queue":               `UPDATE youtube_live_time_finalizations SET queue='manual' WHERE video_id='tf-3'`,
	} {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}

	versions := liveRowVersions(t, pool)

	if err := applyMigrationFile(ctx, pool, dir, timeFinalizationMigration); err != nil {
		t.Fatalf("replay %s: %v", timeFinalizationMigration, err)
	}

	if got := liveRowVersions(t, pool); got != versions {
		t.Fatal("replaying the finalization migration rewrote rows")
	}

	var validated bool

	if err := pool.QueryRow(ctx, `SELECT convalidated FROM pg_constraint
        WHERE conname='chk_youtube_live_sessions_times_source_vocab'`).Scan(&validated); err != nil || !validated {
		t.Fatalf("times_source vocabulary validated = %v, %v", validated, err)
	}
}

// liveRowVersions는 세션·확정 진행 행의 행 버전 목록이다. 재적용이 행을 다시 쓰면 바뀐다.
func liveRowVersions(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var versions string

	if err := pool.QueryRow(t.Context(), `SELECT string_agg(video_id||':'||xmin::text, ',' ORDER BY video_id)
        FROM (SELECT video_id, xmin FROM youtube_live_sessions UNION ALL
              SELECT video_id, xmin FROM youtube_live_time_finalizations) rows`).Scan(&versions); err != nil {
		t.Fatal(err)
	}

	return versions
}
