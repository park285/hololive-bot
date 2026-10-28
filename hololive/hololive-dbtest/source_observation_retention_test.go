package dbtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
)

const communityPageKind = "community_page"

func TestSourceObservationRetentionBatchHonorsGlobalLimitAndProtection(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)

	queueProtected := insertRetentionObservation(t, pool, communityPageKind, base, "queue-protected")
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
		INSERT INTO public.youtube_live_pending_ends (
			video_id, channel_id, kind, observation_id, effective_at, received_at,
			scheduled_for, negative_eligible, scope_covers
		)
		SELECT 'retention-head', 'retention-channel', 'EXPLICIT_END', id, observed_at,
		       received_at, scheduled_for, true, true
		FROM public.source_observations WHERE id = $1`, headProtected); err != nil {
		t.Fatalf("protect retention observation with pending end: %v", err)
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

	assertRetentionObservationsExist(t, pool, []int64{queueProtected, headProtected, recent})
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
	t.Parallel()

	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	lockedID := insertRetentionObservation(t, pool, communityPageKind, base, "locked")
	nextID := insertRetentionObservation(t, pool, communityPageKind, base.Add(time.Hour), "next")

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin retention row lock: %v", err)
	}

	t.Cleanup(func() {
		if rollbackErr := pgxdb.Rollback(t.Context(), tx); rollbackErr != nil {
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

func insertRetentionObservation(
	t *testing.T,
	pool *pgxpool.Pool,
	kind string,
	receivedAt time.Time,
	key string,
) int64 {
	t.Helper()

	const provider = "youtubejs"

	if _, err := pool.Exec(t.Context(), `INSERT INTO public.source_observation_payloads
		(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ($1,1,'source-observation-canonical-json-v1',decode(repeat('b',64),'hex'),'{}')
		ON CONFLICT (observation_kind,schema_version,canonical_profile,payload_sha256) DO NOTHING`, kind); err != nil {
		t.Fatalf("insert retention payload: %v", err)
	}

	var id int64

	if err := pool.QueryRow(t.Context(), `
		INSERT INTO public.source_observations (
			provider, observation_kind, subject_key, observation_key,
			schema_version, contract_generation, scheduled_for, observed_at, received_at,
			scope_sha256, completeness, continuity, payload_id, evidence_sha256,
			provenance_id, fence_epoch, projection_generation
		) VALUES (
			$1, $2, $3, sha256(convert_to($3, 'UTF8')), 1, 1, $4, $4, $4,
			decode(pg_catalog.repeat('a', 64), 'hex'), 'COMPLETE', 'CONTIGUOUS',
			(SELECT id FROM public.source_observation_payloads WHERE observation_kind=$2 AND schema_version=1
			 AND canonical_profile='source-observation-canonical-json-v1' AND payload_sha256=decode(repeat('b',64),'hex')),
			decode(pg_catalog.repeat('c', 64), 'hex'),
			public.source_observation_provenance_id($3, 'dbtest', $2), 1, 1
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

func TestSourceRetentionProtectsProjectionLeaseAndActiveSessions(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	ids := make([]int64, 0, 5)

	for _, key := range []string{"current", "staging", "leased", "live", "upcoming"} {
		ids = append(ids, insertRetentionObservation(t, pool, communityPageKind, base, key))
	}

	eligible := insertRetentionObservation(t, pool, communityPageKind, base, "eligible")
	// generation 번호를 명시하여 세 가지 보호를 서로 독립적으로 검증한다.
	_, err := pool.Exec(t.Context(), `INSERT INTO youtube_collection_projection_generations
		(generation,status,row_count,projection_sha256,valid_until,activated_at) OVERRIDING SYSTEM VALUE
		VALUES (9001,'CURRENT',0,repeat('a',64),now(),now()),
		       (9002,'STAGING',0,repeat('b',64),now(),NULL),
		       (9003,'RETIRED',0,repeat('c',64),now(),now());
		UPDATE source_observations SET projection_generation=CASE subject_key WHEN 'current' THEN 9001
			WHEN 'staging' THEN 9002 ELSE 9003 END;
		INSERT INTO youtube_collection_job_leases
			(job_key,provider,job_class,collection_job_kind,subject_key,projection_generation,poll_interval_ms,
			 slot_state,scheduled_for,next_due_at,owner_instance,lease_expires_at)
		VALUES ('leased','youtubejs','SUBJECT','community_collect','leased',9003,120000,
			'ACTIVE',now(),now(),'collector',now()+interval '1 minute');
		INSERT INTO youtube_live_sessions(video_id,channel_id,status)
		VALUES ('retention-live','UC_retention','LIVE'),('retention-upcoming','UC_retention','UPCOMING');
		INSERT INTO source_observation_applications(observation_id,entity_kind_id,entity_key,decision_id)
		SELECT id,source_observation_entity_kind_id('youtube_live_session'),'retention-'||subject_key,source_observation_decision_id('APPLIED') FROM source_observations
		WHERE subject_key IN ('live','upcoming')`)
	if err != nil {
		t.Fatal(err)
	}

	if got := deleteRetentionBatch(t, pool, []string{communityPageKind}, []time.Time{base.Add(time.Hour)}, 1000); len(got) != 1 || got[0] != eligible {
		t.Fatalf("protected retention deleted %v, want only %d", got, eligible)
	}

	assertRetentionObservationsExist(t, pool, ids)

	_, err = pool.Exec(t.Context(), `UPDATE youtube_collection_projection_generations SET status='RETIRED',activated_at=now();
		UPDATE youtube_collection_job_leases SET slot_state='IDLE',owner_instance=NULL,lease_expires_at=NULL;
		UPDATE youtube_live_sessions SET status='ENDED'`)
	if err != nil {
		t.Fatal(err)
	}

	if got := deleteRetentionBatch(t, pool, []string{communityPageKind}, []time.Time{base.Add(time.Hour)}, 1000); len(got) != len(ids) {
		t.Fatalf("released retention deleted %v, want %v", got, ids)
	}

	var applications int

	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_applications`).Scan(&applications); err != nil {
		t.Fatal(err)
	}

	if applications != 0 {
		t.Fatalf("applications remain after parents expire: %d", applications)
	}
}
