package alarmdispatch

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	retentionPlanRowsPerStatus = 10000
	retentionPlanRetentionDays = 90
	retentionPlanLimit         = 100
)

// 보존 삭제는 상태별 보존 부분 인덱스로 만료 행을 오래된 순서대로 limit만큼만 읽어야 한다. 같은 연결에서
// maintenance가 같은 SQL을 반복 실행하므로 custom plan과 generic plan을 모두 확인한다.
func TestAlarmDispatchRetentionDeleteReadsOnlyExpiredRowsPerStatus(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	seedAlarmDispatchRetentionPlanHistory(t, pool)

	for status, query := range alarmDispatchRetentionDeleteSQL {
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			t.Run(string(status)+"/"+mode, func(t *testing.T) {
				plan, deleted := explainAlarmDispatchRetentionDelete(ctx, t, pool, query, mode,
					fmt.Sprintf("%d, %d", retentionPlanRetentionDays, retentionPlanLimit))
				require.Equal(t, retentionPlanLimit, deleted, "retention delete must remove one full batch")

				stats := retentionPlanStats{}
				stats.collect(&plan)

				assert.False(t, stats.seqScan, "%s %s plan must not sequentially scan deliveries", status, mode)
				// picked는 limit만큼 읽고 바깥 DELETE는 고른 행을 PK로 한 번씩 다시 읽는다.
				assert.LessOrEqual(t, stats.examinedRows, float64(2*retentionPlanLimit),
					"%s %s plan read deliveries beyond the expired batch", status, mode)
			})
		}
	}
}

// seedAlarmDispatchRetentionPlanHistory는 종료 상태마다 만료된 행과 보존 기간 안의 행을 반씩 만든다.
func seedAlarmDispatchRetentionPlanHistory(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()
	eventID := seedAlarmDispatchRetentionEvent(t, pool)

	for status := range alarmDispatchRetentionDeleteSQL {
		// 종료 상태의 시각 열 이름은 "<status>_at"이다.
		column := string(status) + "_at"
		_, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO alarm_dispatch_deliveries (
				event_id, room_id, dedupe_key, status, %s, created_at, updated_at
			)
			SELECT $1, 'retention-plan-room', 'retention-plan-' || $2::text || '-' || n, $2::text, at, at, at
			FROM generate_series(1, $3::int) AS n
			CROSS JOIN LATERAL (
				SELECT NOW() - CASE WHEN n %% 2 = 0 THEN INTERVAL '200 days' ELSE INTERVAL '1 day' END
				       - n * INTERVAL '1 second' AS at
			) AS ts`, column), eventID, string(status), retentionPlanRowsPerStatus)
		require.NoError(t, err, "seed %s history", status)
	}

	_, err := pool.Exec(ctx, `ANALYZE alarm_dispatch_deliveries`)
	require.NoError(t, err)
}

func explainAlarmDispatchRetentionDelete(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	query, mode, params string,
) (retentionPlanNode, int) {
	t.Helper()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)

	// EXPLAIN ANALYZE는 행을 실제로 지우므로 다음 상태·모드가 같은 이력으로 계획되게 되돌린다.
	defer func() { require.NoError(t, tx.Rollback(context.WithoutCancel(ctx))) }()

	_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "PREPARE retention_delete_plan AS "+query)
	require.NoError(t, err)

	// prepared statement는 rollback 뒤에도 연결에 남으므로 풀에 돌려주기 전에 지운다.
	defer func() {
		_, deallocateErr := conn.Exec(context.WithoutCancel(ctx), "DEALLOCATE retention_delete_plan")
		require.NoError(t, deallocateErr)
	}()

	var before, after int

	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&before))

	var planJSON []byte

	require.NoError(t, tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE retention_delete_plan("+params+")").Scan(&planJSON))
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries").Scan(&after))

	var plans []struct {
		Plan retentionPlanNode `json:"Plan"`
	}

	require.NoError(t, jsonv2.Unmarshal(planJSON, &plans))
	require.Len(t, plans, 1)

	return plans[0].Plan, before - after
}

type retentionPlanNode struct {
	NodeType string  `json:"Node Type"`
	Relation string  `json:"Relation Name"`
	Rows     float64 `json:"Actual Rows"`
	Loops    float64 `json:"Actual Loops"`
	// 필터와 lossy bitmap 재검사가 버린 행도 heap에서 읽은 행이다.
	RemovedByFilter  float64             `json:"Rows Removed by Filter"`
	RemovedByRecheck float64             `json:"Rows Removed by Index Recheck"`
	Plans            []retentionPlanNode `json:"Plans"`
}

type retentionPlanStats struct {
	seqScan      bool
	examinedRows float64
}

func (s *retentionPlanStats) collect(node *retentionPlanNode) {
	if node.Relation == "alarm_dispatch_deliveries" && node.NodeType != "ModifyTable" {
		s.examinedRows += (node.Rows + node.RemovedByFilter + node.RemovedByRecheck) * max(node.Loops, 1)

		s.seqScan = s.seqScan || node.NodeType == "Seq Scan"
	}

	for i := range node.Plans {
		s.collect(&node.Plans[i])
	}
}
