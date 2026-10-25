package sourceobservation

import (
	"context"
	"strings"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
)

// 운영 checkpoint 표 규모(약 1.2만 행)를 심어 publish set의 join 전략이 표 크기에 좌우되게 한다.
const publishPlanCheckpointSeedSQL = `
	INSERT INTO public.source_collection_checkpoints (
		provider, observation_kind, subject_key, scope_sha256, contract_generation,
		last_observation_key, last_evidence_sha256, last_scheduled_for, last_success_at,
		collection_latency_ms, continuity, created_at, updated_at
	)
	SELECT 'youtubejs', 'video_list', 'plan-subject-' || (series % 400)::text,
	       pg_catalog.lpad(pg_catalog.to_hex(series), 64, '0'), 1,
	       'checkpoint-' || series::text, pg_catalog.repeat('a', 64), $1::timestamptz, $1::timestamptz,
	       1, 'NOT_APPLICABLE', $1::timestamptz, $1::timestamptz
	FROM pg_catalog.generate_series(1, 12000) AS series
`

// 식별 잠금 함수 lock_source_observation_identity는 uq_source_observation_identity로 최대 1행을 돌려준다. 이를
// planner가 SETOF 기본값 1000행으로 보면 existing CTE가 입력 100행 × 1000으로 과대 추정되어, checkpoint 조인이
// 표 전체 seq scan + 다중 batch hash join으로 풀리고 발행마다 임시 파일이 생긴다(운영 2026-10-24 관측).
func TestPublishSetPlanEstimatesSingleIdentityRowAndProbesCheckpointsByKey(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(t.Context(), publishPlanCheckpointSeedSQL, base); err != nil {
		t.Fatalf("seed checkpoints: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `ANALYZE public.source_collection_checkpoints`); err != nil {
		t.Fatalf("analyze checkpoints: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	rows, err := pool.Query(ctx, "EXPLAIN (COSTS, FORMAT TEXT) "+sqlPublishSet, "[]")
	if err != nil {
		t.Fatalf("explain publish set: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder

	for rows.Next() {
		var line string

		if scanErr := rows.Scan(&line); scanErr != nil {
			t.Fatalf("scan plan line: %v", scanErr)
		}

		plan.WriteString(line)
		plan.WriteByte('\n')
	}

	if rows.Err() != nil {
		t.Fatalf("read plan: %v", rows.Err())
	}

	identityScans := 0

	for line := range strings.Lines(plan.String()) {
		if strings.Contains(line, "Function Scan on lock_source_observation_identity") {
			identityScans++

			if !strings.Contains(line, " rows=1 ") {
				t.Fatalf("identity lock function is not estimated as a single row:\n%s", plan.String())
			}
		}

		if strings.Contains(line, "Seq Scan on source_collection_checkpoints") {
			t.Fatalf("publish set hashes the whole checkpoint table instead of probing it by key:\n%s", plan.String())
		}
	}

	if identityScans == 0 {
		t.Fatalf("plan has no identity lock function scan:\n%s", plan.String())
	}
}
