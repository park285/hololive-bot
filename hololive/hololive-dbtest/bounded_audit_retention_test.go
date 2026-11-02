package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
)

func TestContentAbsenceSlotRetentionWaitsForOldPendingContent(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()
	now := time.Now().UTC()
	cutoff := now.Add(-14 * 24 * time.Hour)
	old := now.Add(-15 * 24 * time.Hour)

	insertContentAbsenceSlot(t, pool, "UC_retention_old", old)
	insertContentAbsenceSlot(t, pool, "UC_retention_new", now.Add(-24*time.Hour))

	observationID := insertRetentionObservation(t, pool, "video_list", old, "old-content-retention")
	if _, err := pool.Exec(ctx, `INSERT INTO public.source_observation_queue (observation_id) VALUES ($1)`, observationID); err != nil {
		t.Fatalf("queue old content observation: %v", err)
	}

	if deleted := countRetentionBatch(t, pool, `
		SELECT count(*) FROM public.delete_youtube_content_absence_slot_retention_batch($1, 1000)`, cutoff); deleted != 0 {
		t.Fatalf("deleted %d content slots while an old video_list observation is pending", deleted)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE public.source_observation_queue SET status='PROCESSED', processed_at=$2 WHERE observation_id=$1
	`, observationID, now); err != nil {
		t.Fatalf("complete old content observation: %v", err)
	}

	if deleted := countRetentionBatch(t, pool, `
		SELECT count(*) FROM public.delete_youtube_content_absence_slot_retention_batch($1, 1000)`, cutoff); deleted != 1 {
		t.Fatalf("deleted %d content slots after completion, want 1", deleted)
	}

	assertRowChannels(t, pool, `SELECT channel_id FROM public.youtube_content_absence_slots`, "UC_retention_new")
}

func TestChannelAuditRetentionDeletesOnlyAgedRows(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()
	now := time.Now().UTC()
	cutoff := now.Add(-30 * 24 * time.Hour)

	for channel, receivedAt := range map[string]time.Time{
		"UC_audit_old": now.Add(-31 * 24 * time.Hour),
		"UC_audit_new": now.Add(-24 * time.Hour),
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO public.youtube_channel_profile_evidence (
				channel_id, scheduled_for, provider, handle_present, description_present,
				country_present, joined_date_present, complete, effective_at, received_at
			) VALUES ($1, $2, 'youtubejs', false, false, false, false, true, $2, $2)
		`, channel, receivedAt); err != nil {
			t.Fatalf("insert profile evidence %s: %v", channel, err)
		}

		if _, err := pool.Exec(ctx, `
			INSERT INTO public.youtube_channel_photo_variants (
				channel_id, kind, provider, scheduled_for, url, effective_at, received_at
			) VALUES ($1, 'avatar', 'youtubejs', $2, 'https://example.invalid/avatar', $2, $2)
		`, channel, receivedAt); err != nil {
			t.Fatalf("insert photo variant %s: %v", channel, err)
		}
	}

	if deleted := countRetentionBatch(t, pool, `
		SELECT count(*) FROM public.delete_youtube_channel_profile_evidence_retention_batch($1, 1000)`, cutoff); deleted != 1 {
		t.Fatalf("deleted %d profile evidence rows, want 1", deleted)
	}

	if deleted := countRetentionBatch(t, pool, `
		SELECT count(*) FROM public.delete_youtube_channel_photo_variant_retention_batch($1, 1000)`, cutoff); deleted != 1 {
		t.Fatalf("deleted %d photo variant rows, want 1", deleted)
	}

	assertRowChannels(t, pool, `SELECT channel_id FROM public.youtube_channel_profile_evidence`, "UC_audit_new")
	assertRowChannels(t, pool, `SELECT channel_id FROM public.youtube_channel_photo_variants`, "UC_audit_new")
}

// resolver가 재사용 중인 사전 행은 건너뛰고, 원본이 참조하는 행은 남기며, 참조 없는 행만 지운다.
func TestProvenanceRetentionDeletesOnlyUnreferencedRows(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	insertRetentionObservation(t, pool, "live_snapshot", time.Now().UTC(), "referenced-job")

	referenced := resolveProvenance(t, pool, "referenced-job")
	orphan := resolveProvenance(t, pool, "orphan-job")
	reused := resolveProvenance(t, pool, "reused-job")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin resolver transaction: %v", err)
	}

	rollbackOnCleanup(t, tx, "resolver")

	var held int

	if err := tx.QueryRow(ctx, `SELECT public.source_observation_provenance_id('reused-job', 'dbtest', 'live_snapshot')`).Scan(&held); err != nil || held != reused {
		t.Fatalf("hold reused provenance = %d, %v; want %d", held, err, reused)
	}

	if deleted := runProvenanceRetention(t, pool); deleted != 1 {
		t.Fatalf("deleted %d provenances while one is held by a resolver, want only the orphan", deleted)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("release resolver transaction: %v", err)
	}

	// 첫 창이 끝까지 지나갔으므로 커서가 처음으로 돌아온 뒤 풀린 행을 지운다.
	deleted := int64(0)

	for range 3 {
		deleted += runProvenanceRetention(t, pool)
	}

	if deleted != 1 {
		t.Fatalf("deleted %d provenances after the resolver released its lock, want 1", deleted)
	}

	remaining := provenanceIDs(t, pool)

	if len(remaining) != 1 || remaining[0] != referenced {
		t.Fatalf("remaining provenances = %v, want only referenced %d (orphan %d, reused %d)", remaining, referenced, orphan, reused)
	}
}

// 정리가 먼저 잠근 사전 행을 resolver가 기다리면, 정리 커밋 뒤 새 행을 만들어 원본 삽입이 FK 위반 없이 끝난다.
func TestProvenanceResolverRecreatesRowDeletedByConcurrentRetention(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()
	// 원본 삽입에 쓸 payload를 만들고, 그 원본이 참조하는 사전 행은 정리 대상에서 빠진다.
	insertRetentionObservation(t, pool, "live_snapshot", time.Now().UTC(), "seed-job")

	deletedID := resolveProvenance(t, pool, "raced-job")

	gc, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin retention transaction: %v", err)
	}

	rollbackOnCleanup(t, gc, "retention")

	var deleted int64

	if err := gc.QueryRow(ctx, `SELECT public.delete_source_observation_provenance_batch(1000)`).Scan(&deleted); err != nil || deleted != 1 {
		t.Fatalf("retention in open transaction = %d, %v; want 1", deleted, err)
	}

	type publishResult struct {
		id  int64
		err error
	}

	published := make(chan publishResult, 1)

	go func() {
		var id int64

		err := pool.QueryRow(ctx, `
			INSERT INTO public.source_observations (
				provider, observation_kind, subject_key, observation_key,
				schema_version, contract_generation, scheduled_for, observed_at, received_at,
				scope_sha256, completeness, continuity, payload_id, evidence_sha256,
				provenance_id, fence_epoch, projection_generation
			)
			SELECT 'youtubejs', 'live_snapshot', 'raced-job', sha256(convert_to('raced-job', 'UTF8')), 1, 1,
				now(), now(), now(), decode(repeat('a', 64), 'hex'), 'COMPLETE', 'CONTIGUOUS', payload.id,
				decode(repeat('c', 64), 'hex'),
				public.source_observation_provenance_id('raced-job', 'dbtest', 'live_snapshot'), 1, 1
			FROM public.source_observation_payloads AS payload
			LIMIT 1
			RETURNING provenance_id`).Scan(&id)
		published <- publishResult{id: id, err: err}
	}()

	waitForLockWaiter(t, pool)

	if err := gc.Commit(ctx); err != nil {
		t.Fatalf("commit retention: %v", err)
	}

	select {
	case got := <-published:
		if got.err != nil {
			t.Fatalf("publish after concurrent retention: %v", got.err)
		}

		if got.id == int64(deletedID) {
			t.Fatalf("publish reused deleted provenance %d", deletedID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("publish did not finish after retention committed")
	}
}

// rollbackOnCleanup은 시험이 커밋하지 못하고 끝난 트랜잭션을 정리한다. 커밋한 트랜잭션에는 아무것도 하지 않는다.
func rollbackOnCleanup(t *testing.T, tx pgx.Tx, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer cancel()

		if err := pgxdb.Rollback(ctx, tx); err != nil {
			t.Errorf("rollback %s transaction: %v", name, err)
		}
	})
}

func insertContentAbsenceSlot(t *testing.T, pool *pgxpool.Pool, channel string, scheduledFor time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO public.youtube_content_absence_slots (
			channel_id, observation_kind, scheduled_for, evidence_sha256, effective_at,
			received_at, scope_sha256, coverage
		) VALUES ($1, 'video_list', $2, decode(repeat('a', 64), 'hex'), $2, $2, decode(repeat('b', 64), 'hex'), '{}'::jsonb)
	`, channel, scheduledFor); err != nil {
		t.Fatalf("insert content absence slot %s: %v", channel, err)
	}
}

func countRetentionBatch(t *testing.T, pool *pgxpool.Pool, query string, cutoff time.Time) int {
	t.Helper()

	var deleted int

	if err := pool.QueryRow(t.Context(), query, cutoff).Scan(&deleted); err != nil {
		t.Fatalf("run retention batch: %v", err)
	}

	return deleted
}

func assertRowChannels(t *testing.T, pool *pgxpool.Pool, query string, want ...string) {
	t.Helper()

	rows, err := pool.Query(t.Context(), query)
	if err != nil {
		t.Fatalf("read remaining rows: %v", err)
	}
	defer rows.Close()

	var got []string

	for rows.Next() {
		var channel string

		if err := rows.Scan(&channel); err != nil {
			t.Fatalf("scan remaining row: %v", err)
		}

		got = append(got, channel)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate remaining rows: %v", err)
	}

	if len(got) != len(want) || (len(want) > 0 && got[0] != want[0]) {
		t.Fatalf("remaining channels = %v, want %v", got, want)
	}
}

func resolveProvenance(t *testing.T, pool *pgxpool.Pool, jobKey string) int {
	t.Helper()

	var id int

	if err := pool.QueryRow(t.Context(), `
		SELECT public.source_observation_provenance_id($1, 'dbtest', 'live_snapshot')`, jobKey).Scan(&id); err != nil {
		t.Fatalf("resolve provenance %s: %v", jobKey, err)
	}

	return id
}

func runProvenanceRetention(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var deleted int64

	if err := pool.QueryRow(t.Context(), `SELECT public.delete_source_observation_provenance_batch(1000)`).Scan(&deleted); err != nil {
		t.Fatalf("run provenance retention: %v", err)
	}

	return deleted
}

func provenanceIDs(t *testing.T, pool *pgxpool.Pool) []int {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT id FROM public.source_observation_provenances ORDER BY id`)
	if err != nil {
		t.Fatalf("read remaining provenances: %v", err)
	}
	defer rows.Close()

	var ids []int

	for rows.Next() {
		var id int

		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan remaining provenance: %v", err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate remaining provenances: %v", err)
	}

	return ids
}

func waitForLockWaiter(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		var waiting bool

		if err := pool.QueryRow(t.Context(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_catalog.pg_stat_activity
				WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()
			)`).Scan(&waiting); err != nil {
			t.Fatalf("read lock waiters: %v", err)
		}

		if waiting {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("resolver did not wait on the provenance row locked by retention")
}
