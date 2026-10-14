package targetprojection

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type projectionCommitTraceKey struct{}

type projectionCacheTracer struct {
	loads     atomic.Int64
	pauseNext atomic.Bool
	committed chan struct{}
	release   chan struct{}
}

func (tr *projectionCacheTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == mustSQL("load_reasons.sql") {
		tr.loads.Add(1)
	}
	if data.SQL == "commit" {
		return context.WithValue(ctx, projectionCommitTraceKey{}, true)
	}

	return ctx
}

func (tr *projectionCacheTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(projectionCommitTraceKey{}) != true || data.Err != nil || !tr.pauseNext.CompareAndSwap(true, false) {
		return
	}

	// 서버 commit 이후, Refresh가 결과를 받아 캐시를 공개하기 전의 경합을 재현한다.
	close(tr.committed)
	select {
	case <-tr.release:
	case <-ctx.Done():
	}
}

func tracedProjectionRefresher(t *testing.T, pool *pgxpool.Pool) (*Refresher, *projectionCacheTracer) {
	t.Helper()

	tr := &projectionCacheTracer{committed: make(chan struct{}), release: make(chan struct{})}
	cfg := pool.Config()
	cfg.ConnConfig.Tracer = tr
	traced, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(traced.Close)
	refresher, err := NewRefresher(traced, time.Hour)
	require.NoError(t, err)

	return refresher, tr
}

func TestReasonsCacheSkipsOnlyConfirmedGenerationAndReasons(t *testing.T) {
	pool := dbtest.NewPool(t)
	refresher, tr := tracedProjectionRefresher(t, pool)
	targets, reasons := capacityTargets(2)
	input := staticBuilder{targets: targets, reasons: reasons}
	first := mustRefresh(t, refresher, input, projectionNow, "initial")
	require.Equal(t, int64(1), tr.loads.Load(), "staging must still verify stored reasons")

	// 같은 집합의 순서·중복은 normalize 이후의 identity를 바꾸지 않는다.
	reordered := staticBuilder{targets: targets, reasons: []TargetReason{reasons[1], reasons[0], reasons[1]}}
	mustRefresh(t, refresher, reordered, projectionNow.Add(time.Second), "canonical heartbeat")
	require.Equal(t, int64(1), tr.loads.Load())

	reasons[0].ReasonKey = "updated"
	got := mustRefresh(t, refresher, input, projectionNow.Add(2*time.Second), "same generation reasons change")
	require.Equal(t, first.Generation, got.Generation)
	require.False(t, got.Changed)
	require.Equal(t, int64(2), tr.loads.Load())
	requireStoredReasons(t, pool, first.Generation, reasons)
	requireEligibilityVersion(t, pool, first.Generation, 1)

	mustRefresh(t, refresher, input, projectionNow.Add(3*time.Second), "changed reasons cached")
	require.Equal(t, int64(2), tr.loads.Load())

	input.reasons = nil
	mustRefresh(t, refresher, input, projectionNow.Add(4*time.Second), "remove all reasons")
	requireStoredReasons(t, pool, first.Generation, nil)
	require.Equal(t, int64(3), tr.loads.Load())
	mustRefresh(t, refresher, input, projectionNow.Add(5*time.Second), "empty reasons cached")
	require.Equal(t, int64(3), tr.loads.Load())

	targets[0].Priority++
	rotated := mustRefresh(t, refresher, input, projectionNow.Add(6*time.Second), "new generation")
	require.NotEqual(t, first.Generation, rotated.Generation)
	require.Equal(t, int64(4), tr.loads.Load(), "new staging always verifies even with the same reasons hash")
	mustRefresh(t, refresher, input, projectionNow.Add(7*time.Second), "new generation cached")
	require.Equal(t, int64(4), tr.loads.Load())
}

func TestReasonsCacheInvalidatesOtherRefresherAtSameHeartbeatClock(t *testing.T) {
	pool := dbtest.NewPool(t)
	refresher, tr := tracedProjectionRefresher(t, pool)
	other, err := NewRefresher(pool, time.Hour)
	require.NoError(t, err)
	targets, reasons := capacityTargets(1)
	input := staticBuilder{targets: targets, reasons: reasons}
	first := mustRefresh(t, refresher, input, projectionNow, "initial")
	foreign := staticBuilder{targets: targets, reasons: slices.Clone(reasons)}
	foreign.reasons[0].ReasonKey = "other writer"
	got := mustRefresh(t, other, foreign, projectionNow, "same timestamp foreign write")
	require.Equal(t, first.Generation, got.Generation)
	requireValidityRefreshedAt(t, pool, got.Generation, projectionNow)
	requireStoredReasons(t, pool, got.Generation, foreign.reasons)

	mustRefresh(t, refresher, input, projectionNow, "restore requested reasons")
	require.Equal(t, int64(2), tr.loads.Load(), "generation and refresh clock alone cannot validate this cache")
	requireStoredReasons(t, pool, first.Generation, reasons)
	requireEligibilityVersion(t, pool, first.Generation, 1)

	mustRefresh(t, refresher, input, projectionNow, "confirmed after foreign writer")
	require.Equal(t, int64(2), tr.loads.Load())

	// 구조 hash가 원래 값으로 돌아와도 퇴역 전 세대의 캐시를 재사용하지 않는다.
	rotatedInput := staticBuilder{targets: slices.Clone(targets), reasons: foreign.reasons}
	rotatedInput.targets[0].Priority++
	mustRefresh(t, other, rotatedInput, projectionNow, "foreign structural change")
	restored := mustRefresh(t, other, foreign, projectionNow, "foreign structural return")
	require.NotEqual(t, first.Generation, restored.Generation)
	got = mustRefresh(t, refresher, input, projectionNow, "same hash on another generation")
	require.Equal(t, restored.Generation, got.Generation)
	require.Equal(t, int64(3), tr.loads.Load())
	requireStoredReasons(t, pool, got.Generation, reasons)
}

func TestReasonsCacheDiscardsFailedCommitAndRollback(t *testing.T) {
	for _, failure := range []string{"statement", "commit", "connection", "panic"} {
		t.Run(failure, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			refresher, tr := tracedProjectionRefresher(t, pool)
			targets, reasons := capacityTargets(1)
			input := staticBuilder{targets: targets, reasons: reasons}
			first := mustRefresh(t, refresher, input, projectionNow, "initial")
			before := projectionTupleVersions(t, pool)
			failed := staticBuilder{targets: slices.Clone(targets), reasons: slices.Clone(reasons)}
			failed.targets[0].NotBefore = projectionNow.Add(time.Minute)
			failed.reasons[0].ReasonKey = "uncommitted"

			switch failure {
			case "statement", "commit":
				_, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_cached_reason_test() RETURNS trigger LANGUAGE plpgsql AS $$
					BEGIN RAISE EXCEPTION 'injected cached reason failure'; END; $$`)
				require.NoError(t, err)
				trigger := `CREATE TRIGGER reject_cached_reason_test BEFORE INSERT ON youtube_collection_target_reasons
					FOR EACH ROW EXECUTE FUNCTION reject_cached_reason_test()`
				if failure == "commit" {
					trigger = `CREATE CONSTRAINT TRIGGER reject_cached_reason_test AFTER INSERT ON youtube_collection_target_reasons
						DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_cached_reason_test()`
				}
				_, err = pool.Exec(t.Context(), trigger)
				require.NoError(t, err)
				_, err = refresher.Refresh(t.Context(), failed, projectionNow.Add(time.Second))
				require.ErrorContains(t, err, "injected cached reason failure")
				if failure == "commit" {
					require.ErrorContains(t, err, "commit pgx transaction")
				}
				_, err = pool.Exec(t.Context(), `DROP TRIGGER reject_cached_reason_test ON youtube_collection_target_reasons`)
				require.NoError(t, err)
			case "connection":
				broken := projectionBuilderFunc(func(ctx context.Context, tx dbx.Tx, _ time.Time) ([]TargetSpec, []TargetReason, error) {
					_, err := tx.Exec(ctx, `SELECT pg_terminate_backend(pg_backend_pid())`)
					return nil, nil, err
				})
				_, err := refresher.Refresh(t.Context(), broken, projectionNow.Add(time.Second))
				require.Error(t, err)
			case "panic":
				broken := projectionBuilderFunc(func(context.Context, dbx.Tx, time.Time) ([]TargetSpec, []TargetReason, error) {
					panic("injected builder panic")
				})
				require.Panics(t, func() { _, _ = refresher.Refresh(t.Context(), broken, projectionNow.Add(time.Second)) })
			}

			require.Equal(t, before, projectionTupleVersions(t, pool), "failed refresh must not publish target, reason, or header changes")
			loads := tr.loads.Load()
			mustRefresh(t, refresher, input, projectionNow.Add(2*time.Second), "reload after uncertain cache")
			require.Equal(t, loads+1, tr.loads.Load(), "failure must discard even the preceding committed cache")
			requireStoredReasons(t, pool, first.Generation, reasons)
			requireEligibilityVersion(t, pool, first.Generation, 1)

			mustRefresh(t, refresher, failed, projectionNow.Add(3*time.Second), "apply previously rolled back change")
			requireStoredReasons(t, pool, first.Generation, failed.reasons)
			requireEligibilityVersion(t, pool, first.Generation, 2)
		})
	}
}

func TestReasonsCacheSerializesPublicationAcrossInterleavedCallers(t *testing.T) {
	pool := dbtest.NewPool(t)
	refresher, tr := tracedProjectionRefresher(t, pool)
	other, err := NewRefresher(pool, time.Hour)
	require.NoError(t, err)
	targets, reasons := capacityTargets(1)
	input := staticBuilder{targets: targets, reasons: reasons}
	first := mustRefresh(t, refresher, input, projectionNow, "initial")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tr.pauseNext.Store(true)
	firstDone := make(chan error, 1)
	go func() {
		_, err := refresher.Refresh(ctx, input, projectionNow.Add(time.Second))
		firstDone <- err
	}()
	select {
	case <-tr.committed:
	case err := <-firstDone:
		t.Fatalf("heartbeat returned before commit barrier: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	// 다른 인스턴스는 DB guard가 풀린 즉시 같은 세대를 바꿀 수 있다.
	foreign := staticBuilder{targets: targets, reasons: slices.Clone(reasons)}
	foreign.reasons[0].ReasonKey = "interleaved foreign writer"
	_, err = other.Refresh(ctx, foreign, projectionNow.Add(time.Second))
	require.NoError(t, err)
	secondBuilt := make(chan struct{})
	secondDone := make(chan error, 1)
	second := projectionBuilderFunc(func(context.Context, dbx.Tx, time.Time) ([]TargetSpec, []TargetReason, error) {
		close(secondBuilt)
		return targets, reasons, nil
	})
	go func() {
		_, err := refresher.Refresh(ctx, second, projectionNow.Add(2*time.Second))
		secondDone <- err
	}()
	select {
	case <-secondBuilt:
		t.Fatal("same refresher entered before the preceding commit was published")
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(tr.release)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	requireStoredReasons(t, pool, first.Generation, reasons)
	require.Equal(t, int64(2), tr.loads.Load(), "late publication must still invalidate the foreign writer")
	mustRefresh(t, refresher, input, projectionNow.Add(3*time.Second), "latest publication remains cached")
	require.Equal(t, int64(2), tr.loads.Load())
}

func TestReasonsCacheBuildFailureForcesReconfirmation(t *testing.T) {
	pool := dbtest.NewPool(t)
	refresher, tr := tracedProjectionRefresher(t, pool)
	targets, reasons := capacityTargets(1)
	input := staticBuilder{targets: targets, reasons: reasons}
	mustRefresh(t, refresher, input, projectionNow, "initial")
	_, err := refresher.Refresh(t.Context(), staticBuilder{err: errors.New("input unavailable")}, projectionNow)
	require.Error(t, err)
	mustRefresh(t, refresher, input, projectionNow, "reconfirm after failure")
	require.Equal(t, int64(2), tr.loads.Load())
}

func requireStoredReasons(t *testing.T, pool *pgxpool.Pool, generation int64, want []TargetReason) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	got, err := loadReasons(t.Context(), tx, generation)
	require.NoError(t, err)
	require.ElementsMatch(t, want, got)
}
