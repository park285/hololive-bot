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
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const claimBacklogCandidateVisitLimit = 128

var claimBacklogCandidateCTEs = map[string]bool{
	"CTE candidates": true,
}

var claimBacklogActiveCTEs = map[string]bool{
	"CTE active_backlog": true,
}

var claimBacklogDeadLetterCTEs = map[string]bool{
	"CTE exhausted_candidates":      true,
	"CTE replay_expired_candidates": true,
}

type claimBacklogPlanNode struct {
	Relation         string                 `json:"Relation Name"`
	Subplan          string                 `json:"Subplan Name"`
	CTE              string                 `json:"CTE Name"`
	Rows             float64                `json:"Actual Rows"`
	Loops            float64                `json:"Actual Loops"`
	RemovedByFilter  float64                `json:"Rows Removed by Filter"`
	RemovedByRecheck float64                `json:"Rows Removed by Index Recheck"`
	Plans            []claimBacklogPlanNode `json:"Plans"`
}

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

func seedClaimBacklog(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	seedClaimBacklogRows(t, pool, 50000, 0)
}

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
			evidence_sha256, provenance_id, fence_epoch, projection_generation
		)
		SELECT provider,
			CASE WHEN n <= $2::int / 25 OR (n > $2::int AND n % 25 = 0) THEN 'shorts_list' ELSE 'video_list' END,
			'UC_backlog_' || (n % 200), sha256(('backlog-' || n)::bytea), schema_version, contract_generation,
			scheduled_for - n * INTERVAL '1 minute', observed_at, scope_sha256,
			completeness, continuity, payload_id, evidence_sha256,
			provenance_id, fence_epoch, projection_generation
		FROM source_observations CROSS JOIN generate_series(1, $2::int + $3::int) AS n
		WHERE id = $1
	`, first, active, history)
	require.NoError(tb, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO source_observation_queue (observation_id, available_at, status, processed_at, attempt_count)
		SELECT id, NOW() - INTERVAL '1 hour' + (id % 1000) * INTERVAL '1 second',
		       CASE WHEN is_history THEN 'PROCESSED' ELSE 'PENDING' END,
		       CASE WHEN is_history THEN NOW() - INTERVAL '30 minutes' END,
		       CASE WHEN is_history THEN 1 ELSE 0 END
		FROM (
			SELECT id, scheduled_for < (SELECT first.scheduled_for FROM source_observations AS first WHERE first.id = $1)
			                - $2::int * INTERVAL '1 minute' AS is_history
			FROM source_observations
			WHERE id <> $1
		) AS seeded
	`, first, active)
	require.NoError(tb, err)

	_, err = pool.Exec(ctx, "ANALYZE source_observations; ANALYZE source_observation_queue")
	require.NoError(tb, err)
}

func explainClaimBacklog(ctx context.Context, pool *pgxpool.Pool, mode, budget string, kinds []string) (raw []byte, err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire: %w", err)
	}

	var tx pgx.Tx

	prepared := false

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)

		defer cancel()

		if tx != nil {
			if rollbackErr := pgxdb.Rollback(cleanupCtx, tx); rollbackErr != nil {
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

	if _, err := tx.Exec(ctx, "PREPARE claim_backlog_plan AS "+claimObservationsSQL); err != nil {
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

	kinds := []string{"video_list", "shorts_list"}

	for _, mode := range planCacheModes {
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
