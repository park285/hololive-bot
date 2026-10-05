package sourceobservation

import (
	"context"
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

// claimBacklogDeadLetterCTEs는 attempts exhausted·replay epoch 만료 분류 CTE다. 시도 소진 분류(exhausted_candidates)는
// materialized 활성 backlog에서 due 행을 골라 queue PK로만 잠그므로 queue를 따로 읽지 않는다. 이전 SQL은 queue를 다시
// 읽어 이 fixture(PG 18.6)에서 활성 행 50,001개를, 활성 20만·PROCESSED 이력 20만 행에서는 이력까지 40만 행을
// 읽었다(BenchmarkClaimBacklog). 활성 epoch가 없으면 replay_expired_candidates도 행을 읽지 않는다. 그래서 두 CTE에
// 후보 잠금 상한을 적용한다.
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

	seedClaimBacklogRows(t, pool, 50000, 0)
}

// seedClaimBacklogRows는 PENDING 활성 행 active개(앞 4%는 shorts, 채널 200개)와 이미 처리된 PROCESSED 이력
// history개(4%는 shorts)를 만든다. 이력은 같은 kind·채널의 과거 관측이라 kind 조건으로는 걸러지지 않는다.
func seedClaimBacklogRows(tb testing.TB, pool *pgxpool.Pool, active, history int) {
	tb.Helper()

	ctx := tb.Context()
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, tb, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(tb, repo, &proof, "known")

	_, err := pool.Exec(ctx, `
		INSERT INTO source_observations (
			provider, observation_kind, subject_key, observation_key, schema_version, contract_generation,
			scheduled_for, observed_at, scope_sha256, completeness, continuity, payload_id,
			evidence_sha256, collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation
		)
		SELECT provider,
			CASE WHEN n <= $2::int / 25 OR (n > $2::int AND n % 25 = 0) THEN 'shorts_list' ELSE 'video_list' END,
			'UC_backlog_' || (n % 200), 'backlog-' || n, schema_version, contract_generation,
			scheduled_for - n * INTERVAL '1 minute', observed_at, scope_sha256,
			completeness, continuity, payload_id, evidence_sha256,
			collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation
		FROM source_observations CROSS JOIN generate_series(1, $2::int + $3::int) AS n
		WHERE id = $1
	`, first, active, history)
	require.NoError(tb, err)

	// observation_key 번호가 active보다 큰 행은 처리 끝난 이력이다.
	_, err = pool.Exec(ctx, `
		INSERT INTO source_observation_queue (observation_id, available_at, status, processed_at, attempt_count)
		SELECT id, NOW() - INTERVAL '1 hour' + (id % 1000) * INTERVAL '1 second',
		       CASE WHEN is_history THEN 'PROCESSED' ELSE 'PENDING' END,
		       CASE WHEN is_history THEN NOW() - INTERVAL '30 minutes' END,
		       CASE WHEN is_history THEN 1 ELSE 0 END
		FROM (
			SELECT id, substr(observation_key, length('backlog-') + 1)::int > $2::int AS is_history
			FROM source_observations
			WHERE id <> $1
		) AS seeded
	`, first, active)
	require.NoError(tb, err)

	_, err = pool.Exec(ctx, "ANALYZE source_observations; ANALYZE source_observation_queue")
	require.NoError(tb, err)
}

// explainClaimBacklog는 PREPARE/EXECUTE로 plan_cache_mode가 실제 generic plan에도 적용되게 한다.
// 준비된 statement는 세션 객체라 롤백으로 사라지지 않고, statement_timeout 등으로 트랜잭션이 중단되면
// 같은 트랜잭션 안에서 DEALLOCATE할 수도 없다(25P02). 그래서 전용 연결에서 롤백한 뒤 트랜잭션 밖에서 해제하고,
// 해제에 실패한 연결은 pool에 돌려주지 않고 닫아 다음 subtest가 같은 이름을 물려받지 않게 한다.
// 원래 오류와 정리 오류는 모두 보존한다.
func explainClaimBacklog(ctx context.Context, pool *pgxpool.Pool, mode, budget string, kinds []string) (raw []byte, err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire: %w", err)
	}

	var tx pgx.Tx

	prepared := false

	// 측정 취소와 분리된 유한 예산에서 롤백한 뒤 세션의 statement를 해제한다.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)

		defer cancel()

		if tx != nil {
			if rollbackErr := tx.Rollback(cleanupCtx); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("rollback explain: %w", rollbackErr))
			}
		}

		if prepared {
			if _, deallocErr := conn.Exec(cleanupCtx, "DEALLOCATE claim_backlog_plan"); deallocErr != nil {
				err = errors.Join(err, fmt.Errorf("deallocate claim: %w", deallocErr))

				if closeErr := conn.Hijack().Close(cleanupCtx); closeErr != nil {
					err = errors.Join(err, fmt.Errorf("close leaked claim connection: %w", closeErr))
				}

				return
			}
		}

		conn.Release()
	}()

	tx, err = conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	if _, err := tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode); err != nil {
		return nil, fmt.Errorf("set plan cache mode: %w", err)
	}

	if _, err := tx.Exec(ctx, "SELECT set_config('statement_timeout', $1, true)", budget); err != nil {
		return nil, fmt.Errorf("set claim budget: %w", err)
	}

	if _, err := tx.Exec(ctx, "PREPARE claim_backlog_plan AS "+mustSQL("repository_claim_0012_12.sql")); err != nil {
		return nil, fmt.Errorf("prepare claim: %w", err)
	}

	prepared = true

	if err := tx.QueryRow(ctx, fmt.Sprintf(
		"EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE claim_backlog_plan('{%s}', 4, 'test-plan', '%s', 60000, %d)",
		strings.Join(kinds, ","), strings.Repeat("a", 64), MaxAttempts,
	)).Scan(&raw); err != nil {
		return nil, fmt.Errorf("explain claim: %w", err)
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
			raw, err := explainClaimBacklog(ctx, pool, mode, "1s", kinds)
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
			require.LessOrEqual(t, claimBacklogVisits(&plans[0].Plan, map[string]bool{"active_backlog": true}, false), 3*activeRows,
				"head selection, candidate sorting and exhausted classification must each read materialized active backlog once, not per candidate")
			require.LessOrEqual(t, claimBacklogVisits(&plans[0].Plan, claimBacklogDeadLetterCTEs, false), float64(claimBacklogCandidateVisitLimit),
				"dead-letter classification must lock exhausted rows from the active pass instead of rescanning the queue")
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
