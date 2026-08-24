package dbtest

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestViewerSampleEvidenceCascadesWithSourceObservation(t *testing.T) {
	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	observationID := insertRetentionObservation(t, pool, "viewer_sample", base, "viewer-cascade")

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO public.youtube_live_viewer_sample_evidence (
			video_id, sample_window_start, provider, observation_id, viewer_count,
			availability, sample_window_seconds, scheduled_for, effective_at, received_at
		) VALUES ('viewer-cascade', $1, 'youtubejs', $2, 100, 'AVAILABLE', 120, $1, $1, $1)
	`, base, observationID); err != nil {
		t.Fatalf("insert viewer evidence: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO public.source_observation_applications (
			observation_id, provider, observation_kind, subject_key, evidence_sha256,
			entity_kind, entity_key, decision, effective_at
		)
		SELECT id, provider, observation_kind, subject_key, evidence_sha256,
		       'youtube_live_viewer_sample', 'viewer-cascade', 'APPLIED', received_at
		FROM public.source_observations
		WHERE id = $1
	`, observationID); err != nil {
		t.Fatalf("insert viewer application: %v", err)
	}

	if got := deleteRetentionBatch(
		t,
		pool,
		[]string{"viewer_sample"},
		[]time.Time{base.Add(time.Hour)},
		1,
	); len(got) != 1 || got[0] != observationID {
		t.Fatalf("deleted observations = %v, want [%d]", got, observationID)
	}

	var evidenceCount int

	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM public.youtube_live_viewer_sample_evidence
		WHERE observation_id = $1
	`, observationID).Scan(&evidenceCount); err != nil {
		t.Fatalf("count viewer evidence: %v", err)
	}

	if evidenceCount != 0 {
		t.Fatalf("viewer evidence count = %d, want 0", evidenceCount)
	}

	var (
		applicationCount             int
		applicationObservationIsNull bool
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT count(*), bool_and(observation_id IS NULL)
		FROM public.source_observation_applications
		WHERE entity_key = 'viewer-cascade'
	`).Scan(&applicationCount, &applicationObservationIsNull); err != nil {
		t.Fatalf("load viewer application audit: %v", err)
	}

	if applicationCount != 1 || !applicationObservationIsNull {
		t.Fatalf("viewer application audit = count %d observation_null %t, want one orphan", applicationCount, applicationObservationIsNull)
	}
}

const checkpointRetentionSeedSQL = `
		INSERT INTO public.source_collection_checkpoints (
			provider, observation_kind, subject_key, scope_sha256, contract_generation,
			last_observation_key, last_evidence_sha256, last_scheduled_for, last_success_at,
			collection_latency_ms, continuity, created_at, updated_at
		)
		SELECT 'youtubejs', 'viewer_sample', 'plan-video',
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

func TestCheckpointRetentionCandidatePlanUsesBoundedIndexes(t *testing.T) {
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

	const index = "idx_source_collection_checkpoints_updated_identity"

	if uses := strings.Count(plan, "using "+index); uses < 2 {
		t.Fatalf("checkpoint retention plan used %s %d times, want candidate and newer lookup:\n%s", index, uses, plan)
	}

	if deleted := countReturnedRows(ctx, t, pool, `
		SELECT deleted_scope_sha256
		FROM public.delete_source_collection_checkpoint_retention_batch($1, 1000)
	`, base); deleted != 1000 {
		t.Fatalf("deleted checkpoints = %d, want 1000", deleted)
	}
}

const applicationRetentionSeedSQL = `
		INSERT INTO public.source_observation_applications (
			observation_id, provider, observation_kind, subject_key, evidence_sha256,
			entity_kind, entity_key, decision, effective_at, applied_at
		)
		SELECT NULL, 'youtubejs', 'community_page', 'UC_PLAN', pg_catalog.repeat('a', 64),
		       'retention_plan', 'application-' || series::text, 'APPLIED', $1::timestamptz,
		       CASE WHEN series <= 22000 THEN $1::timestamptz - interval '100 days' ELSE $1::timestamptz + interval '1 day' END
		FROM pg_catalog.generate_series(1, 24000) AS series
	`

const applicationRetentionExplainSQL = `
		EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
		SELECT application.id, application.applied_at
		FROM public.source_observation_applications AS application
		WHERE application.observation_kind = 'community_page'
		  AND application.observation_id IS NULL
		  AND application.applied_at < $1
		ORDER BY application.applied_at, application.id
		LIMIT 1000
		FOR UPDATE OF application SKIP LOCKED
	`

func TestApplicationRetentionCandidatePlanUsesOrphanPartialIndex(t *testing.T) {
	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(t.Context(), applicationRetentionSeedSQL, base); err != nil {
		t.Fatalf("seed application retention plan: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		ANALYZE public.source_observation_applications
	`); err != nil {
		t.Fatalf("analyze application retention plan: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	plan := explainQueryPlan(ctx, t, pool, applicationRetentionExplainSQL, base)

	const index = "idx_source_observation_applications_orphaned_kind_applied_id"

	if !strings.Contains(plan, index) {
		t.Fatalf("application retention plan did not use %s:\n%s", index, plan)
	}

	if deleted := countReturnedRows(ctx, t, pool, `
		SELECT deleted_id
		FROM public.delete_source_observation_application_retention_batch(
			ARRAY['community_page']::text[], ARRAY[$1]::timestamptz[], 1000
		)
	`, base); deleted != 1000 {
		t.Fatalf("deleted applications = %d, want 1000", deleted)
	}

	if deleted := countReturnedRows(ctx, t, pool, `
		SELECT deleted_id
		FROM public.delete_source_observation_application_retention_batch(
			ARRAY['community_page']::text[], ARRAY[$1]::timestamptz[], 1001
		)
	`, base); deleted != 0 {
		t.Fatalf("invalid application retention budget deleted %d rows, want 0", deleted)
	}
}
