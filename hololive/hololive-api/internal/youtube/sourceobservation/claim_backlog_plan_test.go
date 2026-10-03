package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// claimBacklogCandidateVisitLimit은 LIMIT 4 claim의 후보 잠금 CTE가 읽을 수 있는 행 상한이다.
// 기존 보존 이력 plan test와 같은 128을 쓴다. OR 한 덩어리 후보 CTE는 활성 backlog 전체(약 5만 행)를 정렬한 뒤
// 잘랐고, 같은 fixture에서 claim 1회가 1.2~2.0초(JIT 포함) 걸렸다. 대기열(queue) 순서로 걸으며 차단된 후속 목록을
// 건너뛰는 방식도 채널·종류 선두가 드물게 섞인 이 fixture에서 상한을 넘었다.
const claimBacklogCandidateVisitLimit = 128

var claimBacklogCandidateCTEs = map[string]bool{
	"CTE candidates": true,
}

// claimBacklogActiveCTEs는 채널·종류 선두 판정에 필요한 활성 backlog 단일 pass다. 선두를 정하려면 차단된 후속 목록까지
// 봐야 하므로 backlog에 선형이며, 행마다 backlog를 다시 훑지 않도록 테이블마다 한 번 읽는 선형 상한을 고정한다.
var claimBacklogActiveCTEs = map[string]bool{
	"CTE active_backlog": true,
}

// claimBacklogDeadLetterCTEs는 attempts exhausted·replay epoch 만료 분류 CTE다. 두 CTE는 가지로 나누지 않았다.
// 이 fixture(PG 18.6)에서 exhausted_candidates는 queue를 한 번 순차 스캔해 활성 행 50,001개를 모두 필터로
// 버리고(수 ms), replay_expired_candidates는 활성 epoch가 없어 행을 읽지 않는다. 두 CTE의 attempt_count·epoch 조건에는
// 순서 인덱스가 없어 backlog에 선형이지만 claim당 한 번이므로, 테이블마다 한 번 읽는 선형 상한만 고정한다.
var claimBacklogDeadLetterCTEs = map[string]bool{
	"CTE exhausted_candidates":      true,
	"CTE replay_expired_candidates": true,
}

type claimBacklogPlanNode struct {
	Relation string  `json:"Relation Name"`
	Subplan  string  `json:"Subplan Name"`
	CTE      string  `json:"CTE Name"`
	Rows     float64 `json:"Actual Rows"`
	Loops    float64 `json:"Actual Loops"`
	// 필터와 lossy bitmap 재검사가 버린 행도 heap에서 읽은 행이다. EXPLAIN은 Actual Rows처럼 loop당 평균으로 보고한다.
	RemovedByFilter  float64                `json:"Rows Removed by Filter"`
	RemovedByRecheck float64                `json:"Rows Removed by Index Recheck"`
	Plans            []claimBacklogPlanNode `json:"Plans"`
}

// claimBacklogVisits는 지정한 CTE의 테이블 조회 또는 materialized CTE 재조회에서 반환·폐기한 행을 더한다.
func claimBacklogVisits(node *claimBacklogPlanNode, ctes map[string]bool, inScope bool) float64 {
	inScope = inScope || ctes[node.Subplan]

	visits := float64(0)

	if (inScope && (node.Relation == "source_observation_queue" || node.Relation == "source_observations")) || (node.CTE != "" && ctes[node.CTE]) {
		visits = (node.Rows + node.RemovedByFilter + node.RemovedByRecheck) * node.Loops
	}

	for i := range node.Plans {
		visits += claimBacklogVisits(&node.Plans[i], ctes, inScope)
	}

	return visits
}

// seedClaimBacklog는 API가 오래 멈춘 뒤의 활성 backlog를 만든다: PENDING 5만 행, 그중 shorts 2천 행(채널 200개).
func seedClaimBacklog(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(t, repo, &proof, "known")

	_, err := pool.Exec(ctx, `
		INSERT INTO source_observations (
			provider, observation_kind, subject_key, observation_key, schema_version, contract_generation,
			scheduled_for, observed_at, scope_sha256, completeness, continuity, payload_id,
			evidence_sha256, collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation
		)
		SELECT provider,
			CASE WHEN n <= 2000 THEN 'shorts_list' ELSE 'video_list' END,
			'UC_backlog_' || (n % 200), 'backlog-' || n, schema_version, contract_generation,
			scheduled_for - n * INTERVAL '1 minute', observed_at, scope_sha256,
			completeness, continuity, payload_id, evidence_sha256,
			collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation
		FROM source_observations CROSS JOIN generate_series(1, 50000) AS n
		WHERE id = $1
	`, first)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO source_observation_queue (observation_id, available_at)
		SELECT id, NOW() - INTERVAL '1 hour' + (id % 1000) * INTERVAL '1 second'
		FROM source_observations
		WHERE id <> $1
	`, first)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "ANALYZE source_observations; ANALYZE source_observation_queue")
	require.NoError(t, err)
}

// explainClaimBacklog는 PREPARE/EXECUTE로 plan_cache_mode가 실제 generic plan에도 적용되게 하고,
// 판정 전에 prepared statement를 해제하고 롤백해 연결을 pool에 돌려준다.
func explainClaimBacklog(ctx context.Context, pool *pgxpool.Pool, mode string, kinds []string) (raw []byte, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	// claim은 행을 바꾸므로 측정 뒤 항상 롤백한다.
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback explain: %w", rollbackErr))
		}
	}()

	if _, err := tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode); err != nil {
		return nil, fmt.Errorf("set plan cache mode: %w", err)
	}

	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '1s'"); err != nil {
		return nil, fmt.Errorf("set claim budget: %w", err)
	}

	if _, err := tx.Exec(ctx, "PREPARE claim_backlog_plan AS "+mustSQL("repository_claim_0012_12.sql")); err != nil {
		return nil, fmt.Errorf("prepare claim: %w", err)
	}

	explainErr := tx.QueryRow(ctx, fmt.Sprintf(
		"EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE claim_backlog_plan('{%s}', 4, 'test-plan', '%s', 60000, %d)",
		strings.Join(kinds, ","), strings.Repeat("a", 64), MaxAttempts,
	)).Scan(&raw)

	if _, err := tx.Exec(ctx, "DEALLOCATE claim_backlog_plan"); err != nil {
		return nil, fmt.Errorf("deallocate claim: %w", err)
	}

	if explainErr != nil {
		return nil, fmt.Errorf("explain claim: %w", explainErr)
	}

	return raw, nil
}

func TestClaimCandidateSelectionIsBoundedUnderActiveBacklog(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedClaimBacklog(t, pool)

	var activeRows float64

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM source_observation_queue").Scan(&activeRows))

	// 두 목록 kind의 채널별 선두 선택을 함께 검증한다. 실제 plane은 다른 kind도 같은 claim에 포함한다.
	kinds := []string{"video_list", "shorts_list"}

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := explainClaimBacklog(ctx, pool, mode, kinds)
			require.NoError(t, err)

			var plans []struct {
				Plan claimBacklogPlanNode `json:"Plan"`
			}

			require.NoError(t, jsonv2.Unmarshal(raw, &plans))
			require.Len(t, plans, 1)
			require.InDelta(t, 4, plans[0].Plan.Rows, 0, "backlog claim must still fill the batch")
			require.LessOrEqual(t, claimBacklogVisits(&plans[0].Plan, claimBacklogCandidateCTEs, false), float64(claimBacklogCandidateVisitLimit),
				"candidate row-lock lookups must stop near LIMIT; active head selection and sorting have separate linear input budgets")
			require.LessOrEqual(t, claimBacklogVisits(&plans[0].Plan, claimBacklogActiveCTEs, false), 2*activeRows,
				"active head materialization must read each table at most once instead of rescanning the backlog per row")
			require.LessOrEqual(t, claimBacklogVisits(&plans[0].Plan, map[string]bool{"active_backlog": true}, false), 2*activeRows,
				"head selection and candidate sorting must not rescan materialized active backlog per candidate")
			require.LessOrEqual(t, claimBacklogVisits(&plans[0].Plan, claimBacklogDeadLetterCTEs, false), 2*activeRows,
				"dead-letter classification must read each table at most once instead of rescanning the backlog per row")
		})
	}

	// 실제 claim은 queue 순서에서 앞서는 채널·종류 선두 4개여야 한다. 기대값은 claim SQL의 선두 계산과 독립적으로
	// "더 앞선 활성 같은 채널·종류 목록이 없음"을 직접 판정해 구한다.
	rows, err := pool.Query(ctx, `
		SELECT queue.observation_id
		FROM source_observation_queue AS queue
		JOIN source_observations AS observation ON observation.id = queue.observation_id
		WHERE queue.status = 'PENDING'
		  AND queue.available_at <= NOW()
		  AND NOT EXISTS (
			  SELECT 1
			  FROM source_observation_queue AS predecessor_queue
			  JOIN source_observations AS predecessor ON predecessor.id = predecessor_queue.observation_id
			  WHERE predecessor_queue.status IN ('PENDING', 'PROCESSING')
			    AND predecessor.subject_key = observation.subject_key
			    AND predecessor.observation_kind = observation.observation_kind
			    AND (predecessor.scheduled_for, predecessor.id) < (observation.scheduled_for, observation.id)
		  )
		ORDER BY queue.available_at, queue.observation_id
		LIMIT 4
	`)
	require.NoError(t, err)

	wantHeads, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	require.NoError(t, err)
	require.Len(t, wantHeads, 4)

	options := contentClaimOptions()

	options.Limit = 4

	batch, err := NewRepository(pool).ClaimBatch(ctx, options)
	require.NoError(t, err)

	gotIDs := make([]int64, 0, len(batch.Claims))
	for _, claim := range batch.Claims {
		gotIDs = append(gotIDs, claim.ObservationID)
	}

	require.ElementsMatch(t, wantHeads, gotIDs, "backlog claim must take the earliest claimable channel/kind heads in queue order")
}
