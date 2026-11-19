package durability

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	prunePlanRetainedRows = 20000
	prunePlanExpiredRows  = 5
	prunePlanBatchSize    = 100
)

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

	defer func() { require.NoError(t, pgxdb.Rollback(ctx, tx)) }()

	_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "PREPARE durable_prune_plan(bigint, bigint, integer) AS "+durablePruneTerminalSQL)
	require.NoError(t, err)

	explain := fmt.Sprintf("EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE durable_prune_plan(%d, %d, %d)",
		int64(8*24*time.Hour/time.Millisecond), int64(30*24*time.Hour/time.Millisecond), prunePlanBatchSize)

	var planJSON []byte

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
