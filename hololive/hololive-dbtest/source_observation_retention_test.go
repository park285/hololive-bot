package dbtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const communityPageKind = "community_page"

func TestSourceObservationRetentionBatchHonorsGlobalLimitAndProtection(t *testing.T) {
	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	queueProtected := insertRetentionObservation(t, pool, communityPageKind, base, "queue-protected")
	replayProtected := insertRetentionObservation(t, pool, communityPageKind, base.Add(time.Hour), "replay-protected")
	headProtected := insertRetentionObservation(t, pool, "live_snapshot", base.Add(2*time.Hour), "head-protected")
	oldestEligible := insertRetentionObservation(t, pool, "live_snapshot", base.Add(3*time.Hour), "oldest-eligible")
	nextEligible := insertRetentionObservation(t, pool, communityPageKind, base.Add(4*time.Hour), "next-eligible")
	recent := insertRetentionObservation(t, pool, "live_snapshot", base.Add(48*time.Hour), "recent")

	if _, err := pool.Exec(t.Context(),
		`INSERT INTO public.source_observation_queue (observation_id) VALUES ($1)`, queueProtected,
	); err != nil {
		t.Fatalf("protect retention observation with queue row: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO public.source_observation_replay_requests (
			observation_id, provider, observation_kind, subject_key, observation_key,
			evidence_sha256, requested_by, reason, previous_attempt_count
		)
		SELECT id, provider, observation_kind, subject_key, observation_key,
		       evidence_sha256, 'dbtest', 'retention protection', 0
		FROM public.source_observations
		WHERE id = $1`, replayProtected); err != nil {
		t.Fatalf("protect retention observation with replay row: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO public.youtube_live_reconciliation_heads (
			video_id, status, end_candidate_kind, end_candidate_observation_id, next_end_check_at
		) VALUES ('retention-head', 'LIVE', 'EXPLICIT_END', $1, $2)`, headProtected, base); err != nil {
		t.Fatalf("protect retention observation with reconciliation head: %v", err)
	}

	kinds := []string{communityPageKind, "live_snapshot"}
	cutoffs := []time.Time{base.Add(24 * time.Hour), base.Add(24 * time.Hour)}

	if got := deleteRetentionBatch(t, pool, kinds, cutoffs, 1); len(got) != 1 || got[0] != oldestEligible {
		t.Fatalf("first retention batch deleted %v, want [%d]", got, oldestEligible)
	}

	if got := deleteRetentionBatch(t, pool, kinds, cutoffs, 1000); len(got) != 1 || got[0] != nextEligible {
		t.Fatalf("second retention batch deleted %v, want [%d]", got, nextEligible)
	}

	assertRetentionObservationsExist(t, pool, []int64{queueProtected, replayProtected, headProtected, recent})
}

func TestSourceObservationRetentionBatchRejectsInvalidBudgets(t *testing.T) {
	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	observationID := insertRetentionObservation(t, pool, communityPageKind, base, "guarded")
	cutoff := base.Add(24 * time.Hour)

	tests := []struct {
		name    string
		kinds   []string
		cutoffs []time.Time
		limit   int
	}{
		{name: "empty policies", kinds: []string{}, cutoffs: []time.Time{}, limit: 1},
		{name: "mismatched policies", kinds: []string{communityPageKind}, cutoffs: []time.Time{}, limit: 1},
		{name: "zero limit", kinds: []string{communityPageKind}, cutoffs: []time.Time{cutoff}, limit: 0},
		{name: "oversized limit", kinds: []string{communityPageKind}, cutoffs: []time.Time{cutoff}, limit: 1001},
		{name: "too many policies", kinds: repeatedStrings(communityPageKind, 17), cutoffs: repeatedTimes(cutoff, 17), limit: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deleteRetentionBatch(t, pool, test.kinds, test.cutoffs, test.limit); len(got) != 0 {
				t.Fatalf("invalid retention request deleted %v", got)
			}
		})
	}

	assertRetentionObservationsExist(t, pool, []int64{observationID})
}

func TestSourceObservationRetentionBatchSkipsLockedRows(t *testing.T) {
	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	lockedID := insertRetentionObservation(t, pool, communityPageKind, base, "locked")
	nextID := insertRetentionObservation(t, pool, communityPageKind, base.Add(time.Hour), "next")

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin retention row lock: %v", err)
	}

	t.Cleanup(func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(t.Context())); rollbackErr != nil {
			t.Errorf("rollback retention row lock: %v", rollbackErr)
		}
	})

	var selectedID int64

	if err := tx.QueryRow(t.Context(),
		`SELECT id FROM public.source_observations WHERE id = $1 FOR UPDATE`, lockedID,
	).Scan(&selectedID); err != nil {
		t.Fatalf("lock retention observation: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	got := deleteRetentionBatchContext(ctx, t, pool,
		[]string{communityPageKind}, []time.Time{base.Add(24 * time.Hour)}, 1)
	if len(got) != 1 || got[0] != nextID {
		t.Fatalf("retention with locked oldest row deleted %v, want [%d]", got, nextID)
	}

	assertRetentionObservationsExist(t, pool, []int64{lockedID})
}

const retentionPlanSeedObservationsSQL = `
		INSERT INTO public.source_observations (
			provider, observation_kind, subject_key, observation_key,
			schema_version, contract_generation, scheduled_for, observed_at, received_at,
			scope_sha256, completeness, continuity, payload, payload_sha256, evidence_sha256,
			collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation
		)
		SELECT 'youtubejs', seed.observation_kind, seed.observation_key, seed.observation_key,
		       1, 1, seed.received_at, seed.received_at, seed.received_at,
		       pg_catalog.repeat('a', 64), 'COMPLETE', 'CONTIGUOUS', '{}'::jsonb,
		       pg_catalog.repeat('b', 64), pg_catalog.repeat('c', 64),
		       'dbtest', seed.observation_key, seed.observation_kind, 1, 1
		FROM (
			SELECT 'community_page'::text AS observation_kind,
			       'plan-community-' || series::text AS observation_key,
			       CASE WHEN series <= 2000 THEN $1::timestamptz - interval '1 day' ELSE $1::timestamptz + interval '1 day' END AS received_at
			FROM pg_catalog.generate_series(1, 22000) AS series
			UNION ALL
			SELECT 'live_snapshot'::text,
			       'plan-live-' || series::text,
			       $1::timestamptz - interval '1 day'
			FROM pg_catalog.generate_series(1, 20000) AS series
		) AS seed`

const retentionPlanSeedQueueSQL = `
		INSERT INTO public.source_observation_queue (observation_id)
		SELECT id
		FROM public.source_observations
		WHERE observation_kind = 'community_page'
		  AND received_at < $1
		ORDER BY received_at, id
		LIMIT 1000`

const retentionCandidateExplainSQL = `
		EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
		WITH policies AS (
			SELECT ($1::text[])[policy.position] AS observation_kind,
			       ($2::timestamptz[])[policy.position] AS cutoff
			FROM pg_catalog.generate_subscripts($1::text[], 1) AS policy(position)
		),
		per_policy_candidates AS (
			SELECT candidate.id, candidate.received_at
			FROM policies AS policy
			CROSS JOIN LATERAL (
				SELECT observation.id, observation.received_at
				FROM public.source_observations AS observation
				WHERE observation.observation_kind = policy.observation_kind
				  AND observation.received_at < policy.cutoff
				  AND NOT EXISTS (
					  SELECT 1 FROM public.source_observation_queue AS queue
					  WHERE queue.observation_id = observation.id
				  )
				  AND NOT EXISTS (
					  SELECT 1 FROM public.source_observation_replay_requests AS replay
					  WHERE replay.observation_id = observation.id AND replay.status = 'PENDING'
				  )
				  AND NOT EXISTS (
					  SELECT 1 FROM public.youtube_live_reconciliation_heads AS head
					  WHERE head.end_candidate_observation_id = observation.id
				  )
				ORDER BY observation.received_at, observation.id
				LIMIT $3
				FOR UPDATE OF observation SKIP LOCKED
			) AS candidate
		)
		SELECT candidate.id
		FROM per_policy_candidates AS candidate
		ORDER BY candidate.received_at, candidate.id
		LIMIT $3`

func TestSourceObservationRetentionCandidatePlanUsesKindReceivedIndex(t *testing.T) {
	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	seedRetentionCandidatePlanFixture(t, pool, base)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	plan := explainQueryPlan(ctx, t, pool, retentionCandidateExplainSQL,
		[]string{communityPageKind}, []time.Time{base}, 1000)

	assertRetentionCandidatePlanUsesKindReceivedIndex(t, plan)

	deleted := deleteRetentionBatchContext(ctx, t, pool,
		[]string{communityPageKind}, []time.Time{base}, 1000)
	if len(deleted) != 1000 {
		t.Fatalf("retention plan fixture deleted %d rows, want 1000", len(deleted))
	}

	var protected int

	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM public.source_observations
		WHERE observation_kind = 'community_page'
		  AND received_at < $1`, base).Scan(&protected); err != nil {
		t.Fatalf("count queue-protected retention plan observations: %v", err)
	}

	if protected != 1000 {
		t.Fatalf("queue-protected retention plan observations = %d, want 1000", protected)
	}
}

func seedRetentionCandidatePlanFixture(t *testing.T, pool *pgxpool.Pool, base time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), retentionPlanSeedObservationsSQL, base); err != nil {
		t.Fatalf("seed retention plan observations: %v", err)
	}

	if _, err := pool.Exec(t.Context(), retentionPlanSeedQueueSQL, base); err != nil {
		t.Fatalf("seed retention plan queue protection: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `ANALYZE public.source_observations; ANALYZE public.source_observation_queue`); err != nil {
		t.Fatalf("analyze retention plan fixtures: %v", err)
	}
}

func assertRetentionCandidatePlanUsesKindReceivedIndex(t *testing.T, plan string) {
	t.Helper()

	indexConditionFound := false

	for line := range strings.SplitSeq(plan, "\n") {
		if strings.Contains(line, "Index Cond:") &&
			strings.Contains(line, "observation_kind") && strings.Contains(line, "received_at") {
			indexConditionFound = true

			break
		}
	}

	if !strings.Contains(plan, "idx_source_observations_kind_received_id") || !indexConditionFound {
		t.Fatalf("retention candidate plan did not constrain the kind/received index:\n%s", plan)
	}
}

func insertRetentionObservation(
	t *testing.T,
	pool *pgxpool.Pool,
	kind string,
	receivedAt time.Time,
	key string,
) int64 {
	t.Helper()

	const provider = "youtubejs"

	var id int64

	if err := pool.QueryRow(t.Context(), `
		INSERT INTO public.source_observations (
			provider, observation_kind, subject_key, observation_key,
			schema_version, contract_generation, scheduled_for, observed_at, received_at,
			scope_sha256, completeness, continuity, payload, payload_sha256, evidence_sha256,
			collector_instance, job_key, collection_job_kind, fence_epoch, projection_generation
		) VALUES (
			$1, $2, $3, $3, 1, 1, $4, $4, $4,
			pg_catalog.repeat('a', 64), 'COMPLETE', 'CONTIGUOUS', '{}'::jsonb,
			pg_catalog.repeat('b', 64), pg_catalog.repeat('c', 64),
			'dbtest', $3, $2, 1, 1
		)
		RETURNING id`, provider, kind, key, receivedAt).Scan(&id); err != nil {
		t.Fatalf("insert retention observation %q: %v", key, err)
	}

	return id
}

func deleteRetentionBatch(
	t *testing.T,
	pool *pgxpool.Pool,
	kinds []string,
	cutoffs []time.Time,
	limit int,
) []int64 {
	t.Helper()

	return deleteRetentionBatchContext(t.Context(), t, pool, kinds, cutoffs, limit)
}

func deleteRetentionBatchContext(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	kinds []string,
	cutoffs []time.Time,
	limit int,
) []int64 {
	t.Helper()

	rows, err := pool.Query(ctx, `
		SELECT deleted_id
		FROM public.delete_source_observation_retention_batch($1::text[], $2::timestamptz[], $3)`,
		kinds, cutoffs, limit)
	if err != nil {
		t.Fatalf("run source observation retention batch: %v", err)
	}
	defer rows.Close()

	var deleted []int64

	for rows.Next() {
		var id int64

		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan source observation retention result: %v", err)
		}

		deleted = append(deleted, id)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate source observation retention result: %v", err)
	}

	return deleted
}

func assertRetentionObservationsExist(t *testing.T, pool *pgxpool.Pool, ids []int64) {
	t.Helper()

	var count int

	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM public.source_observations WHERE id = ANY($1::bigint[])`, ids,
	).Scan(&count); err != nil {
		t.Fatalf("count retained source observations: %v", err)
	}

	if count != len(ids) {
		t.Fatalf("retained source observation count = %d, want %d for ids %v", count, len(ids), ids)
	}
}

func explainQueryPlan(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder

	for rows.Next() {
		var line string

		if scanErr := rows.Scan(&line); scanErr != nil {
			t.Fatalf("scan query plan line: %v", scanErr)
		}

		plan.WriteString(line)
		plan.WriteByte('\n')
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("iterate query plan: %v", rowsErr)
	}

	return plan.String()
}

func countReturnedRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("run retention batch: %v", err)
	}
	defer rows.Close()

	count := 0

	for rows.Next() {
		count++
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("iterate retention batch rows: %v", rowsErr)
	}

	return count
}

func repeatedStrings(value string, count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = value
	}

	return values
}

func repeatedTimes(value time.Time, count int) []time.Time {
	values := make([]time.Time, count)
	for index := range values {
		values[index] = value
	}

	return values
}
