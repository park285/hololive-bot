package store

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
	claimPlanHistoryRows   = 20000
	claimPlanCandidateRows = 5
	claimPlanBatchSize     = 10
)

type claimIndexPlanCase struct {
	name     string
	sqlFile  string
	params   string
	relation string
}

// claimIndexPlanCases는 상태 상수 조건으로 PENDING·SENDING 부분 인덱스(idx_yno_pending_due_created_id,
// idx_ynd_pending_due_created_id, idx_ynd_sending_stale)를 증명하도록 쓴 claim SQL이다.
var claimIndexPlanCases = []claimIndexPlanCase{
	{
		name: "fanout claim", sqlFile: "fanout_claim.sql",
		params: fmt.Sprintf("'PENDING', NOW() - INTERVAL '1 minute', NOW(), NOW() - INTERVAL '1 hour', %d, NOW()",
			claimPlanBatchSize),
		relation: "youtube_notification_outbox",
	},
	{
		name: "claim pending", sqlFile: "transition_claim_pending.sql",
		params: fmt.Sprintf("'PENDING', NOW() - INTERVAL '1 minute', NOW(), NOW() - INTERVAL '1 hour', %d, NOW()",
			claimPlanBatchSize),
		relation: "youtube_notification_delivery",
	},
	{
		name: "stale sending", sqlFile: "transition_stale_sending.sql",
		params:   fmt.Sprintf("'SENDING', NOW() - INTERVAL '1 hour', %d", claimPlanBatchSize),
		relation: "youtube_notification_delivery",
	},
}

// terminal 이력이 많아도 claim SQL은 상태 조건을 담은 인덱스로 후보만 읽어야 한다. 운영 풀은 cache_statement로
// 같은 prepared statement를 반복 실행하므로 custom plan과 기본 plan_cache_mode(auto)가 여러 번 실행된 뒤 고르는
// 계획을 함께 고정한다. 강제 generic plan(force_generic_plan)도 상수 상태 조건 덕분에 같은 범위만 읽으므로 함께
// 요구한다. 인덱스 이름은 고정하지 않는다. 예를 들어 created_at 하한을 아는 fanout custom plan은
// idx_yno_status_created(status, created_at)를 고르지만, status 동등 조건으로 PENDING 행만 읽으므로 결과가 같다.
func TestClaimSQLReadsCandidatesWithoutScanningTerminalHistory(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	seedClaimPlanHistory(ctx, t, pool)

	for _, tc := range claimIndexPlanCases {
		for mode, executions := range map[string]int{planModeForceCustom: 1, "auto": 8, planModeForceGeneric: 1} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				plan := explainClaimSQL(ctx, t, pool, tc, mode, executions)
				stats := claimIndexPlanStats{}
				stats.collect(&plan)

				for _, relation := range []string{"youtube_notification_outbox", "youtube_notification_delivery"} {
					assert.False(t, stats.seqScans[relation], "%s %s plan must not sequentially scan %s", tc.name, mode, relation)
				}

				assert.True(t, stats.indexLookups[tc.relation], "%s %s plan must use an index on %s", tc.name, mode, tc.relation)
				assert.LessOrEqual(t, stats.examinedRows[tc.relation], float64(4*claimPlanBatchSize),
					"%s %s plan examined terminal %s history", tc.name, mode, tc.relation)
			})
		}
	}
}

// seedClaimPlanHistory는 terminal outbox·delivery 이력과 SQL별 후보 몇 행을 만든다.
// PENDING outbox 후보는 delivery가 없고, delivery 후보의 parent outbox는 delivery가 있어 fanout 후보가 아니다.
func seedClaimPlanHistory(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	statements := []string{
		`INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload, status, created_at, sent_at, terminal_at, next_attempt_at)
		SELECT 'NEW_VIDEO', 'UC_PLAN', 'plan-history-' || n, '{}'::jsonb, 'SENT',
		       clock_timestamp() - interval '10 minutes', clock_timestamp(), clock_timestamp(), clock_timestamp() - interval '10 minutes'
		FROM generate_series(1, $1::int) AS n`,
		`INSERT INTO youtube_notification_delivery (outbox_id, room_id, status, created_at, next_attempt_at, sent_at, locked_at)
		SELECT id, 'room-plan', CASE WHEN id % 10 = 0 THEN 'FAILED' ELSE 'SENT' END,
		       created_at, created_at, CASE WHEN id % 10 = 0 THEN NULL ELSE clock_timestamp() END,
		       clock_timestamp() - interval '2 hours'
		FROM youtube_notification_outbox WHERE content_id LIKE 'plan-history-%'`,
		`INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload, status, created_at, next_attempt_at)
		SELECT 'NEW_VIDEO', 'UC_PLAN', 'plan-' || state || '-' || n, '{}'::jsonb, 'PENDING',
		       clock_timestamp() - interval '5 minutes', clock_timestamp() - interval '5 minutes'
		FROM generate_series(1, $1::int) AS n CROSS JOIN (VALUES ('fanout'), ('pending'), ('sending')) AS states(state)`,
		`INSERT INTO youtube_notification_delivery (outbox_id, room_id, status, created_at, next_attempt_at, locked_at)
		SELECT id, 'room-plan', CASE WHEN content_id LIKE 'plan-pending-%' THEN 'PENDING' ELSE 'SENDING' END,
		       created_at, created_at,
		       CASE WHEN content_id LIKE 'plan-sending-%' THEN clock_timestamp() - interval '2 hours' END
		FROM youtube_notification_outbox WHERE content_id LIKE 'plan-pending-%' OR content_id LIKE 'plan-sending-%'`,
		`ANALYZE youtube_notification_outbox, youtube_notification_delivery`,
	}

	for i, statement := range statements {
		var args []any

		switch i {
		case 0:
			args = []any{claimPlanHistoryRows}
		case 2:
			args = []any{claimPlanCandidateRows}
		}

		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err, "seed claim plan statement %d", i)
	}
}

func explainClaimSQL(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	tc claimIndexPlanCase,
	mode string,
	executions int,
) claimIndexPlanNode {
	t.Helper()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)

	// EXPLAIN ANALYZE는 후보를 실제로 잠그고 갱신하므로 트랜잭션을 되돌려 다음 모드가 같은 데이터로 계획되게 한다.
	defer func() { require.NoError(t, tx.Rollback(context.WithoutCancel(ctx))) }()

	_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "PREPARE claim_index_plan AS "+mustSQL(tc.sqlFile))
	require.NoError(t, err)

	// prepared statement는 rollback 뒤에도 연결에 남으므로 풀에 돌려주기 전에 지운다.
	defer func() {
		_, deallocateErr := conn.Exec(context.WithoutCancel(ctx), "DEALLOCATE claim_index_plan")
		require.NoError(t, deallocateErr)
	}()

	var planJSON []byte

	// auto 모드는 custom plan 5회 뒤부터 generic plan과 비용을 비교하므로 마지막 실행의 계획을 본다.
	// 실행마다 savepoint로 되돌려 매번 같은 후보를 읽게 한다.
	for range executions {
		_, err = tx.Exec(ctx, "SAVEPOINT claim_index_plan")
		require.NoError(t, err)
		require.NoError(t, tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE claim_index_plan("+tc.params+")").Scan(&planJSON))

		_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT claim_index_plan")
		require.NoError(t, err)
	}

	var plans []struct {
		Plan claimIndexPlanNode `json:"Plan"`
	}

	require.NoError(t, jsonv2.Unmarshal(planJSON, &plans))
	require.Len(t, plans, 1)

	return plans[0].Plan
}

type claimIndexPlanNode struct {
	NodeType string  `json:"Node Type"`
	Relation string  `json:"Relation Name"`
	Rows     float64 `json:"Actual Rows"`
	Loops    float64 `json:"Actual Loops"`
	// 필터와 lossy bitmap 재검사가 버린 행도 heap에서 읽은 행이다.
	RemovedByFilter  float64              `json:"Rows Removed by Filter"`
	RemovedByRecheck float64              `json:"Rows Removed by Index Recheck"`
	Plans            []claimIndexPlanNode `json:"Plans"`
}

type claimIndexPlanStats struct {
	seqScans     map[string]bool
	indexLookups map[string]bool
	examinedRows map[string]float64
}

func (s *claimIndexPlanStats) collect(node *claimIndexPlanNode) {
	if s.seqScans == nil {
		s.seqScans, s.indexLookups, s.examinedRows = map[string]bool{}, map[string]bool{}, map[string]float64{}
	}

	if node.Relation != "" && node.NodeType != "ModifyTable" {
		s.examinedRows[node.Relation] += (node.Rows + node.RemovedByFilter + node.RemovedByRecheck) * max(node.Loops, 1)

		s.seqScans[node.Relation] = s.seqScans[node.Relation] || node.NodeType == "Seq Scan"
		s.indexLookups[node.Relation] = s.indexLookups[node.Relation] ||
			node.NodeType == "Index Scan" || node.NodeType == "Index Only Scan" || node.NodeType == "Bitmap Heap Scan"
	}

	for i := range node.Plans {
		s.collect(&node.Plans[i])
	}
}
