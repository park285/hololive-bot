package sourceobservation

import (
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type shortsClaimPlanNode struct {
	Relation string                `json:"Relation Name"`
	Rows     float64               `json:"Actual Rows"`
	Loops    float64               `json:"Actual Loops"`
	Plans    []shortsClaimPlanNode `json:"Plans"`
}

func TestShortsClaimWorkIsBoundedByActiveQueueNotRetainedHistory(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(t, repo, &proof, "known")

	// 한 채널의 긴 이력과 다수의 짧은 이력을 섞어 correlated subject 추정의 운영 편향을 재현합니다.
	_, err := pool.Exec(ctx, `
		INSERT INTO source_observations (
			provider, observation_kind, subject_key, observation_key, schema_version, contract_generation,
			scheduled_for, observed_at, scope_sha256, completeness, continuity, payload, payload_sha256,
			evidence_sha256, collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation
		)
		SELECT provider, observation_kind,
			CASE WHEN n <= 10000 THEN subject_key ELSE 'UC_history_' || n END,
			'history-' || n, schema_version, contract_generation,
			scheduled_for - n * INTERVAL '1 minute', observed_at, scope_sha256,
			completeness, continuity, payload, payload_sha256, evidence_sha256,
			collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation
		FROM source_observations CROSS JOIN generate_series(1, 30000) AS n
		WHERE id = $1
	`, first)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO source_observation_queue (observation_id, status, processed_at)
		SELECT id, 'PROCESSED', NOW() FROM source_observations
		WHERE subject_key = $1 AND id <> $2
	`, testChannelID, first)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO source_observation_queue (observation_id, available_at)
		SELECT id, NOW() + INTERVAL '1 hour' FROM source_observations
		WHERE subject_key <> $1 ORDER BY id LIMIT 32
	`, testChannelID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "ANALYZE source_observations; ANALYZE source_observation_queue")
	require.NoError(t, err)

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer func() { require.NoError(t, tx.Rollback(ctx)) }()

			_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode)
			require.NoError(t, err)

			var raw []byte

			err = tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+mustSQL("repository_claim_0012_12.sql"),
				[]string{"shorts_list"}, 1, "test-plan", strings.Repeat("a", 64), int64(60000), MaxAttempts).Scan(&raw)
			require.NoError(t, err)

			var plans []struct {
				Plan shortsClaimPlanNode `json:"Plan"`
			}

			require.NoError(t, jsonv2.Unmarshal(raw, &plans))
			require.Len(t, plans, 1)
			require.LessOrEqual(t, shortsObservationVisits(plans[0].Plan), float64(128),
				"claim must inspect active observations, not the retained history of the channel")
		})
	}
}

func shortsObservationVisits(node shortsClaimPlanNode) float64 {
	visits := float64(0)

	if node.Relation == "source_observations" {
		visits = node.Rows * node.Loops
	}

	for _, child := range node.Plans {
		visits += shortsObservationVisits(child)
	}

	return visits
}
