package alarmdispatch

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	retentionPlanRowsPerStatus = 10000
	retentionPlanRetentionDays = 90
	retentionPlanLimit         = 100
)

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
				assert.LessOrEqual(t, stats.examinedRows, float64(2*retentionPlanLimit),
					"%s %s plan read deliveries beyond the expired batch", status, mode)
			})
		}
	}
}

func seedAlarmDispatchRetentionPlanHistory(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()
	eventID := seedAlarmDispatchRetentionEvent(t, pool)

	var unitID int64

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO alarm_dispatch_send_units (unit_key, dispatch_group_key, room_id, client_request_id)
		VALUES (repeat('d', 64), 'retention-plan-group', 'retention-plan-room', 'retention-plan-unit')
		RETURNING id`).Scan(&unitID))

	for status := range alarmDispatchRetentionDeleteSQL {
		column := string(status) + "_at"
		_, err := pool.Exec(ctx, fmt.Sprintf(`INSERT INTO alarm_dispatch_deliveries (
				event_id, room_id, dedupe_key, dispatch_group_key, send_unit_id, status, %s, created_at, updated_at
			)
			SELECT $1, 'retention-plan-room', 'retention-plan-' || $2::text || '-' || n,
			       'retention-plan-group', $4, $2::text, at, at, at
			FROM generate_series(1, $3::int) AS n
			CROSS JOIN LATERAL (
				SELECT NOW() - CASE WHEN n %% 2 = 0 THEN INTERVAL '200 days' ELSE INTERVAL '1 day' END
				       - n * INTERVAL '1 second' AS at
			) AS ts`, column), eventID, string(status), retentionPlanRowsPerStatus, unitID)
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

	defer func() { require.NoError(t, pgxdb.Rollback(ctx, tx)) }()

	_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "PREPARE retention_delete_plan AS "+query)
	require.NoError(t, err)

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
	NodeType         string              `json:"Node Type"`
	Relation         string              `json:"Relation Name"`
	Rows             float64             `json:"Actual Rows"`
	Loops            float64             `json:"Actual Loops"`
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
