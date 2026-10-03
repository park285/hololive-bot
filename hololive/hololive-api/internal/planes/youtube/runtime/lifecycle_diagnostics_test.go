package runtime

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestLifecycleDiagnosticsSeparateMissingHeadsAndExactReview(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, collectionTargetFixture); err != nil {
		t.Fatal(err)
	}

	_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,lifecycle_origin)
        VALUES ('diag-metadata','fresh','UPCOMING','','metadata_only'),
               ('diag-observed','fresh','UPCOMING','','observed'),
               ('diag-live','fresh','LIVE','','metadata_only'),
               ('diag-legacy','fresh','UPCOMING','','legacy_unknown');
        INSERT INTO youtube_video_availability
        (video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,scheduled_for,effective_at,observed_at,received_at)
        VALUES ('diag-legacy','fresh','youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),now(),now(),now(),now())`)
	if err != nil {
		t.Fatal(err)
	}

	before := observeLifecycleMetrics(t, pool)
	if got := testutil.ToFloat64(before.liveReview.WithLabelValues("state_mismatch")); got != 5 {
		t.Fatalf("actual defects = %v, want 5 including missing LIVE/observed UPCOMING", got)
	}

	recordDiagnosticReview(t, pool)

	after := observeLifecycleMetrics(t, pool)
	if testutil.ToFloat64(after.lifecycleRecords.WithLabelValues("closed_unresolved")) != 1 ||
		testutil.ToFloat64(before.lifecycleRecords.WithLabelValues("legacy_unreviewed"))-testutil.ToFloat64(after.lifecycleRecords.WithLabelValues("legacy_unreviewed")) != 1 ||
		testutil.ToFloat64(after.lifecycleRecords.WithLabelValues("metadata_only")) != 1 ||
		testutil.ToFloat64(after.lifecycleRecords.WithLabelValues("retained_total")) != 11 {
		t.Fatal("review closure changed retained totals or excused metadata/actual defects")
	}

	if _, err := pool.Exec(ctx, `UPDATE youtube_live_sessions SET title='new metadata' WHERE video_id='diag-legacy'`); err != nil {
		t.Fatal(err)
	}

	changed := observeLifecycleMetrics(t, pool)
	if testutil.ToFloat64(changed.lifecycleRecords.WithLabelValues("closed_unresolved")) != 0 ||
		testutil.ToFloat64(changed.liveReview.WithLabelValues("state_mismatch")) != 5 ||
		testutil.ToFloat64(changed.lifecycleRecords.WithLabelValues("legacy_unreviewed")) != testutil.ToFloat64(before.lifecycleRecords.WithLabelValues("legacy_unreviewed")) {
		t.Fatal("changed snapshot was exempted by a stale receipt")
	}
}

func observeLifecycleMetrics(t *testing.T, pool *pgxpool.Pool) *collectionTargetMetrics {
	t.Helper()

	ctx := t.Context()

	rows, queryErr := pool.Query(ctx, mustSQL("collection_target_observability.sql"), defaultLiveFreshnessBudget().Milliseconds())
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	defer rows.Close()

	samples, scanErr := scanCollectionTargets(rows)
	if scanErr != nil {
		t.Fatal(scanErr)
	}

	// 새 registry가 DB 영수증과 원본만으로 동일한 현재 집계를 복원한다.
	metrics := newCollectionTargetMetrics(prometheus.NewPedanticRegistry())
	metrics.observe(samples, time.Now(), nil)

	return metrics
}

func recordDiagnosticReview(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()

	var snapshot string

	if err := pool.QueryRow(ctx, `SELECT snapshot_sha256 FROM youtube_live_review_snapshot('diag-legacy')`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}

	tx, beginErr := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if beginErr != nil {
		t.Fatal(beginErr)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Error(rollbackErr)
		}
	}()

	if _, err := tx.Exec(ctx, `SELECT record_youtube_live_review('20000000-0000-0000-0000-000000000001','diag-legacy',$1,'test-operator','UNKNOWN 검토 종료')`, snapshot); err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
