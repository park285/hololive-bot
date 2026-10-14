package targetprojection

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestExtendTargetsCurrentRangeBufferBound(t *testing.T) {
	pool := dbtest.NewPool(t)
	refresher, err := NewRefresher(pool, time.Hour)
	require.NoError(t, err)

	targets, reasons := capacityTargets(600)
	first := mustRefresh(t, refresher, staticBuilder{targets: targets, reasons: reasons}, projectionNow, "600 current targets")

	seedRetiredTargetPlanPopulation(t, pool, first.Generation)

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer func() {
				if rollbackErr := tx.Rollback(t.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
					t.Error(rollbackErr)
				}
			}()

			require.NoError(t, lockProjectionGuard(t.Context(), tx))

			_, err = tx.Exec(t.Context(), "SET LOCAL plan_cache_mode = "+mode)
			require.NoError(t, err)

			_, err = tx.Exec(t.Context(), "PREPARE extend_target_plan(bigint,text[],text[],timestamptz[]) AS "+mustSQL("extend_targets.sql"))
			require.NoError(t, err)

			before := targetPlanTuples(t, tx, first.Generation)

			targets[0].NotBefore = projectionNow.Add(time.Minute)

			execute := extendTargetsPlanExecution(first.Generation, targets)

			var raw []byte

			require.NoError(t, tx.QueryRow(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+execute).Scan(&raw))
			requireExtendTargetBufferBound(t, raw)

			after := targetPlanTuples(t, tx, first.Generation)
			require.Equal(t, 1, changedTargetPlanTuples(before, after), "only changed eligibility may create a new tuple")

			var changed int

			require.NoError(t, tx.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_targets WHERE projection_generation=$1 AND not_before=$2`, first.Generation, targets[0].NotBefore).Scan(&changed))
			require.Equal(t, 1, changed)

			// 같은 입력의 다음 heartbeat도 쓰기 없이 동일한 범위 예산을 지킨다.
			require.NoError(t, tx.QueryRow(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+execute).Scan(&raw))
			requireExtendTargetBufferBound(t, raw)
			require.Equal(t, after, targetPlanTuples(t, tx, first.Generation))

			assertPreparedExtendPlanCounts(t, tx, mode)

			_, err = tx.Exec(t.Context(), "DEALLOCATE extend_target_plan")
			require.NoError(t, err)
		})
	}

	// EXPLAIN 실행도 rollback되어 CURRENT membership·header와 RETIRED 이력은 보존된다.
	requireEligibilityVersion(t, pool, first.Generation, 1)
	requireRetiredTargetEligibility(t, pool)
}

func assertPreparedExtendPlanCounts(t *testing.T, tx pgx.Tx, mode string) {
	t.Helper()

	var generic, custom int64

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT generic_plans,custom_plans FROM pg_prepared_statements WHERE name='extend_target_plan'`).Scan(&generic, &custom))

	if mode == "force_generic_plan" {
		require.Equal(t, int64(2), generic)
		require.Zero(t, custom)
	} else {
		require.Equal(t, int64(2), custom)
		require.Zero(t, generic)
	}
}

func seedRetiredTargetPlanPopulation(t *testing.T, pool *pgxpool.Pool, generation int64) {
	t.Helper()

	// 과거 세대가 충분히 있어야 전체 테이블 스캔과 CURRENT 범위 스캔의 차이가 드러난다.
	_, err := pool.Exec(t.Context(), `
		WITH retired AS (
			INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
			SELECT 'RETIRED',row_count,projection_sha256,valid_until,activated_at
			FROM youtube_collection_projection_generations CROSS JOIN generate_series(1,80)
			WHERE generation=$1 RETURNING generation
		)
		INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,member_since_generation,created_at,not_before)
		SELECT retired.generation,target.subject_key,target.observation_kind,target.priority,target.poll_interval_ms,target.enabled,
		       target.member_since_generation,target.created_at,target.not_before
		FROM retired CROSS JOIN youtube_collection_targets target WHERE target.projection_generation=$1`, generation)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `ANALYZE youtube_collection_targets`)
	require.NoError(t, err)
}

func extendTargetsPlanExecution(generation int64, targets []TargetSpec) string {
	subjects := make([]string, len(targets))
	kinds := make([]string, len(targets))
	timestamps := make([]string, len(targets))

	for i, target := range targets {
		// 합성 fixture 값만 SQL literal로 사용한다. 운영 입력은 항상 bind parameter다.
		subjects[i] = "'" + target.SubjectKey + "'"
		kinds[i] = "'" + string(target.ObservationKind) + "'"
		timestamps[i] = "NULL"

		if !target.NotBefore.IsZero() {
			timestamps[i] = "'" + target.NotBefore.Format(time.RFC3339Nano) + "'"
		}
	}

	return fmt.Sprintf("EXECUTE extend_target_plan(%d,ARRAY[%s]::text[],ARRAY[%s]::text[],ARRAY[%s]::timestamptz[])",
		generation, strings.Join(subjects, ","), strings.Join(kinds, ","), strings.Join(timestamps, ","))
}

func requireExtendTargetBufferBound(t *testing.T, raw []byte) {
	t.Helper()

	var plans []struct {
		Plan struct {
			Hits       int64 `json:"Shared Hit Blocks"`
			Reads      int64 `json:"Shared Read Blocks"`
			TempReads  int64 `json:"Temp Read Blocks"`
			TempWrites int64 `json:"Temp Written Blocks"`
		} `json:"Plan"`
	}

	require.NoError(t, jsonv2.Unmarshal(raw, &plans))
	require.Len(t, plans, 1)

	plan := plans[0].Plan
	require.LessOrEqual(t, plan.Hits+plan.Reads, int64(100), "600 targets must use a current-generation range scan: %s", raw)
	require.Zero(t, plan.TempReads+plan.TempWrites)
	t.Logf("shared hit=%d read=%d", plan.Hits, plan.Reads)
}

func targetPlanTuples(t *testing.T, tx pgx.Tx, generation int64) map[string]string {
	t.Helper()

	rows, err := tx.Query(t.Context(), `SELECT subject_key||':'||observation_kind,xmin::text||':'||ctid::text
		FROM youtube_collection_targets WHERE projection_generation=$1`, generation)
	require.NoError(t, err)

	defer rows.Close()

	out := make(map[string]string)

	for rows.Next() {
		var key, tuple string

		require.NoError(t, rows.Scan(&key, &tuple))

		out[key] = tuple
	}

	require.NoError(t, rows.Err())

	return out
}

func changedTargetPlanTuples(before, after map[string]string) int {
	changed := 0

	for key, tuple := range after {
		if before[key] != tuple {
			changed++
		}
	}

	return changed
}

func requireRetiredTargetEligibility(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_targets target
		JOIN youtube_collection_projection_generations generation ON generation.generation=target.projection_generation
		WHERE generation.status='RETIRED' AND target.not_before IS NOT NULL`).Scan(&count))
	require.Zero(t, count)
}
