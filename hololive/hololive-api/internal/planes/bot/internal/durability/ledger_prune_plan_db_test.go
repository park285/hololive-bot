package durability

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	prunePlanRetainedRows = 20000
	prunePlanExpiredRows  = 5
	prunePlanBatchSize    = 100
)

// 보존 기간 안의 terminal 이력이 많아도 prune은 terminal 부분 인덱스로 만료 행만 읽어야 한다.
// 운영 풀은 cache_statement로 같은 prepared statement를 15초마다 다시 실행하므로, custom plan과
// 기본 plan_cache_mode(auto)가 여러 번 실행된 뒤 고르는 계획을 함께 고정한다. 기본 모드는 generic
// plan 비용이 custom plan 평균보다 높으면 custom plan을 유지한다. 강제 generic plan(force_generic_plan)에서도
// 세 candidate CTE는 cutoff Index Cond로 terminal 부분 인덱스를 쓴다. 다만 LIMIT $3 값을 모르는 플래너가
// candidate를 수백 행(cutoff 선택도 1/3에 LIMIT 기본 10%)으로 추정해 DELETE ... USING 조인만 Hash Join과
// seq scan을 고르므로, 그 모드는 여기서 요구하지 않는다.
func TestDurablePruneUsesTerminalIndexesInsteadOfScanningRetainedHistory(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := t.Context()
	truncateDurabilityTables(ctx, t, pool)
	seedPrunePlanLedgers(ctx, t, pool)

	for mode, executions := range map[string]int{"force_custom_plan": 1, "auto": 8} {
		t.Run(mode, func(t *testing.T) {
			plan := explainDurablePrune(ctx, t, pool, mode, executions)

			for _, relation := range []string{"bot_webhook_inbox", "bot_command_executions", "bot_reply_outbox"} {
				stats := prunePlanRelationStats(plan, relation)
				assert.False(t, stats.SeqScan, "%s prune plan must not sequentially scan %s", mode, relation)
				assert.True(t, stats.IndexLookup, "%s prune plan must use an index on %s", mode, relation)
				assert.LessOrEqual(t, stats.ExaminedRows, float64(prunePlanBatchSize),
					"%s prune plan examined retained %s history", mode, relation)
			}
		})
	}
}

func seedPrunePlanLedgers(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	statements := []string{
		`INSERT INTO bot_webhook_inbox (message_id, room_id, ordering_key, payload, status, updated_at)
		SELECT 'message:prune-plan-' || n, 'room-1', 'room:prune-plan', '{}'::jsonb, 'succeeded',
		       clock_timestamp() - CASE WHEN n <= $2 THEN interval '9 days' ELSE interval '1 day' END
		FROM generate_series(1, $1::int) AS n`,
		`INSERT INTO bot_command_executions (message_id, command_kind, status, claim_token, result_summary, completed_at, updated_at)
		SELECT 'message:prune-plan-' || n, 'webhook', 'succeeded', 'plan-token', 'succeeded', clock_timestamp(),
		       clock_timestamp() - CASE WHEN n <= $2 THEN interval '9 days' ELSE interval '1 day' END
		FROM generate_series(1, $1::int) AS n`,
		`INSERT INTO bot_reply_outbox (message_id, phase, ordinal, room_id, payload, payload_hash, client_request_id, status, updated_at)
		SELECT 'message:prune-plan-' || n, 'final', 0, 'room-1',
		       CASE WHEN n % 10 = 0 THEN '{"kind":"text","message":"review"}'::jsonb END,
		       repeat('a', 64), 'prune-plan-' || lpad(n::text, 8, '0'),
		       CASE WHEN n % 10 = 0 THEN 'manual_review' ELSE 'handoff_completed' END,
		       clock_timestamp() - CASE WHEN n <= $2 THEN interval '9 days' ELSE interval '1 day' END
		FROM generate_series(1, $1::int) AS n`,
		`ANALYZE bot_webhook_inbox, bot_command_executions, bot_reply_outbox`,
	}

	for i, statement := range statements {
		var args []any

		if i < 3 {
			args = []any{prunePlanRetainedRows + prunePlanExpiredRows, prunePlanExpiredRows}
		}

		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err, "seed prune plan statement %d", i)
	}
}

func explainDurablePrune(ctx context.Context, t *testing.T, pool *pgxpool.Pool, mode string, executions int) map[string]any {
	t.Helper()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)

	// EXPLAIN ANALYZE는 실제로 삭제하므로 트랜잭션을 되돌려 다음 모드가 같은 데이터로 계획되게 한다.
	defer func() { require.NoError(t, tx.Rollback(context.WithoutCancel(ctx))) }()

	_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "PREPARE durable_prune_plan(bigint, bigint, integer) AS "+durablePruneTerminalSQL)
	require.NoError(t, err)

	explain := fmt.Sprintf("EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE durable_prune_plan(%d, %d, %d)",
		int64(8*24*time.Hour/time.Millisecond), int64(30*24*time.Hour/time.Millisecond), prunePlanBatchSize)

	var planJSON []byte

	// auto 모드는 custom plan 5회 뒤부터 generic plan과 비용을 비교하므로 마지막 실행의 계획을 본다.
	for range executions {
		require.NoError(t, tx.QueryRow(ctx, explain).Scan(&planJSON))
	}

	_, err = tx.Exec(ctx, "DEALLOCATE durable_prune_plan")
	require.NoError(t, err)

	var plan []map[string]any

	require.NoError(t, jsonv2.Unmarshal(planJSON, &plan))
	require.NotEmpty(t, plan)

	return plan[0]
}

func prunePlanRelationStats(node map[string]any, relation string) claimPlanStats {
	if plan := planMapValue(node, "Plan"); plan != nil {
		return prunePlanRelationStats(plan, relation)
	}

	stats := claimPlanStats{}

	nodeType := planStringValue(node, "Node Type")
	if node["Relation Name"] == relation && nodeType != "ModifyTable" {
		loops := max(planFloatValue(node, "Actual Loops"), 1)
		actual := planFloatValue(node, "Actual Rows")
		filtered := planFloatValue(node, "Rows Removed by Filter")
		rechecked := planFloatValue(node, "Rows Removed by Index Recheck")

		stats.ExaminedRows = (actual + filtered + rechecked) * loops
		stats.SeqScan = nodeType == "Seq Scan"
		stats.IndexLookup = nodeType == "Index Scan" || nodeType == "Index Only Scan" || nodeType == "Bitmap Heap Scan"
	}

	for _, child := range planSliceValue(node, "Plans") {
		childStats := prunePlanRelationStats(planNodeValue(child), relation)

		stats.ExaminedRows += childStats.ExaminedRows

		stats.SeqScan = stats.SeqScan || childStats.SeqScan
		stats.IndexLookup = stats.IndexLookup || childStats.IndexLookup
	}

	return stats
}
