package dbtest

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestApplicationAuditCascadesWithSourceObservation(t *testing.T) {
	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	id := insertRetentionObservation(t, pool, communityPageKind, base, "audit-cascade")

	if _, err := pool.Exec(t.Context(), `INSERT INTO source_observation_applications
		(observation_id,entity_kind,entity_key,decision)
		VALUES ($1,'community_window','audit-cascade','APPLIED')`, id); err != nil {
		t.Fatal(err)
	}

	if got := deleteRetentionBatch(t, pool, []string{communityPageKind}, []time.Time{base.Add(time.Hour)}, 1); len(got) != 1 || got[0] != id {
		t.Fatalf("deleted observations = %v, want [%d]", got, id)
	}

	var remaining int

	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_applications
		WHERE entity_key='audit-cascade'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}

	if remaining != 0 {
		t.Fatalf("remaining audit rows = %d, want 0", remaining)
	}
}

const checkpointRetentionSeedSQL = `
		INSERT INTO public.source_collection_checkpoints (
			provider, observation_kind, subject_key, scope_sha256, contract_generation,
			last_observation_key, last_evidence_sha256, last_scheduled_for, last_success_at,
			collection_latency_ms, continuity, created_at, updated_at
		)
		SELECT 'youtubejs', 'video_list', 'plan-video',
		       pg_catalog.lpad(pg_catalog.to_hex(series), 64, '0'), 1,
		       'checkpoint-' || series::text, pg_catalog.repeat('a', 64), $1::timestamptz, $1::timestamptz,
		       1, 'NOT_APPLICABLE',
		       CASE WHEN series <= 22000 THEN $1::timestamptz - interval '10 days' ELSE $1::timestamptz + interval '1 day' END,
		       CASE WHEN series <= 22000 THEN $1::timestamptz - interval '10 days' ELSE $1::timestamptz + interval '1 day' END
		FROM pg_catalog.generate_series(1, 24000) AS series
	`

const checkpointRetentionExplainSQL = `
		EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
		SELECT checkpoint.provider,
		       checkpoint.observation_kind,
		       checkpoint.subject_key,
		       checkpoint.scope_sha256
		FROM public.source_collection_checkpoints AS checkpoint
		WHERE checkpoint.updated_at < $1
		  AND EXISTS (
		      SELECT 1
		      FROM public.source_collection_checkpoints AS newer
		      WHERE newer.provider = checkpoint.provider
		        AND newer.observation_kind = checkpoint.observation_kind
		        AND newer.subject_key = checkpoint.subject_key
		        AND (newer.updated_at, newer.scope_sha256) >
		            (checkpoint.updated_at, checkpoint.scope_sha256)
		  )
		ORDER BY checkpoint.updated_at,
		         checkpoint.provider,
		         checkpoint.observation_kind,
		         checkpoint.subject_key,
		         checkpoint.scope_sha256
		LIMIT 1000
		FOR UPDATE OF checkpoint SKIP LOCKED
	`

// scope가 한 subject에 몰린 폭증 분포(오래된 scope 2.2만 개)에서도 보존 후보 선택은 후보마다 checkpoint를 다시
// 순차 스캔하면 안 된다. 267이 updated_at 인덱스를 지운 뒤 PG 18.6은 두 테이블을 한 번씩 읽는 hash semi join을
// 고르고(약 30 ms), 후보마다 seq scan을 반복하는 계획은 행 수의 제곱 비용이 된다.
func TestCheckpointRetentionCandidatePlanReadsCheckpointsOnce(t *testing.T) {
	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(t.Context(), checkpointRetentionSeedSQL, base); err != nil {
		t.Fatalf("seed checkpoint retention plan: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		ANALYZE public.source_collection_checkpoints
	`); err != nil {
		t.Fatalf("analyze checkpoint retention plan: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	plan := explainQueryPlan(ctx, t, pool, checkpointRetentionExplainSQL, base)

	for line := range strings.Lines(plan) {
		if strings.Contains(line, "Seq Scan on source_collection_checkpoints") && !strings.Contains(line, " loops=1)") {
			t.Fatalf("checkpoint retention plan rescans checkpoints per candidate:\n%s", plan)
		}
	}

	if deleted := countReturnedRows(ctx, t, pool, `
		SELECT deleted_scope_sha256
		FROM public.delete_source_collection_checkpoint_retention_batch($1, 1000)
	`, base); deleted != 1000 {
		t.Fatalf("deleted checkpoints = %d, want 1000", deleted)
	}
}
