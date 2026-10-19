package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

// planCacheModes는 plan 시험이 고정하는 PostgreSQL plan_cache_mode다. 운영 풀은 prepared statement를 쓰므로
// 같은 연결에서 반복되면 generic plan으로 바뀔 수 있다.
var planCacheModes = []string{"force_custom_plan", "force_generic_plan"}

type queueRetentionPlanNode struct {
	NodeType         string                   `json:"Node Type"`
	Relation         string                   `json:"Relation Name"`
	Index            string                   `json:"Index Name"`
	Rows             float64                  `json:"Actual Rows"`
	Loops            float64                  `json:"Actual Loops"`
	RemovedByFilter  float64                  `json:"Rows Removed by Filter"`
	RemovedByRecheck float64                  `json:"Rows Removed by Index Recheck"`
	Plans            []queueRetentionPlanNode `json:"Plans"`
}

type queueRetentionPlanStats struct {
	seqScan  bool
	indexes  []string
	examined float64
}

func (s *queueRetentionPlanStats) collect(node *queueRetentionPlanNode) {
	if node.Relation == "source_observation_queue" {
		s.seqScan = s.seqScan || node.NodeType == "Seq Scan"

		s.examined += (node.Rows + node.RemovedByFilter + node.RemovedByRecheck) * node.Loops
	}

	if node.Index != "" {
		s.indexes = append(s.indexes, node.Index)
	}

	for i := range node.Plans {
		s.collect(&node.Plans[i])
	}
}

// queue 보존 삭제는 만료된 PROCESSED·DEAD_LETTER 행만 처리·dead-letter 시각 부분 인덱스(302·303)로 찾아야 한다.
// 인덱스가 없으면 PROCESSED 이력 전체를 읽고 updated_at으로 정렬했다(운영 2026-10-12: 약 22만 행, 평균 166 ms).
// 후보 CTE와 바깥 DELETE가 만료 행을 한 번씩 읽으므로 상한은 만료 행의 두 배다.
func TestQueueRetentionDeleteReadsOnlyExpiredRows(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	// 이력 2만 행 중 1%는 만료된 PROCESSED, 2%는 DEAD_LETTER이고 그 절반이 만료됐다.
	seedClaimBacklogRows(t, pool, 0, 20000)

	_, err := pool.Exec(ctx, `
		UPDATE source_observation_queue
		SET status = 'DEAD_LETTER', processed_at = NULL,
		    dead_lettered_at = NOW() - CASE WHEN observation_id / 50 % 2 = 0 THEN INTERVAL '60 days' ELSE INTERVAL '1 hour' END
		WHERE observation_id % 50 = 1;
		UPDATE source_observation_queue SET processed_at = NOW() - INTERVAL '30 days'
		WHERE status = 'PROCESSED' AND observation_id % 100 = 0;
		ANALYZE source_observation_queue`)
	require.NoError(t, err)

	before := time.Now().Add(-7 * 24 * time.Hour)

	var expired int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_queue
		WHERE (status = 'PROCESSED' AND processed_at < $1) OR (status = 'DEAD_LETTER' AND dead_lettered_at < $1)`,
		before).Scan(&expired))
	require.Positive(t, expired)

	for _, mode := range planCacheModes {
		t.Run(mode, func(t *testing.T) {
			stats := explainQueueRetentionDelete(t, pool, mode, before, 1000)

			require.False(t, stats.seqScan, "queue retention sequentially scanned source_observation_queue")
			require.Contains(t, stats.indexes, "idx_source_observation_queue_processed_retention")
			require.Contains(t, stats.indexes, "idx_source_observation_queue_dead_letter_retention")
			require.LessOrEqual(t, stats.examined, float64(2*expired), "queue retention read rows beyond the expired ones")
		})
	}
}

// explainQueueRetentionDelete는 운영처럼 이름 있는 prepared statement로 실행하고, 실제로 지운 행은 롤백한다.
func explainQueueRetentionDelete(t *testing.T, pool *pgxpool.Pool, mode string, before time.Time, limit int) queueRetentionPlanStats {
	t.Helper()

	ctx := t.Context()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	_, err = conn.Exec(ctx, "SET plan_cache_mode = "+mode)
	require.NoError(t, err)

	_, err = conn.Exec(ctx, "PREPARE queue_retention_plan(timestamptz, timestamptz, integer) AS "+mustSQL("repository_retention_delete_queue_0076_76.sql"))
	require.NoError(t, err)

	defer func() {
		_, resetErr := conn.Exec(context.WithoutCancel(ctx), "DEALLOCATE queue_retention_plan; RESET plan_cache_mode")
		require.NoError(t, resetErr)
	}()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(ctx, tx)) }()

	var raw []byte

	cutoff := before.UTC().Format(time.RFC3339Nano)

	require.NoError(t, tx.QueryRow(ctx, fmt.Sprintf("EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE queue_retention_plan('%s', '%s', %d)",
		cutoff, cutoff, limit)).Scan(&raw))

	var plans []struct {
		Plan queueRetentionPlanNode `json:"Plan"`
	}

	require.NoError(t, jsonv2.Unmarshal(raw, &plans))
	require.Len(t, plans, 1)

	stats := queueRetentionPlanStats{}
	stats.collect(&plans[0].Plan)

	return stats
}
