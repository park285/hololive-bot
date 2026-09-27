package dbtest

import (
	"testing"
	"time"
)

func TestLiveAbsenceSlotRetentionWaitsForActiveOldSnapshot(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()
	now := time.Now().UTC()
	old := now.Add(-31 * 24 * time.Hour)
	observationID := insertRetentionObservation(t, pool, "live_snapshot", old, "old-live-retention")

	if _, err := pool.Exec(ctx, `
		INSERT INTO public.source_observation_queue (observation_id) VALUES ($1)
	`, observationID); err != nil {
		t.Fatalf("insert pending old snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO public.youtube_live_absence_slots (
			observation_id, scheduled_for, evidence_sha256, effective_at,
			received_at, scope_sha256, coverage
		) VALUES (91004, $1, repeat('a', 64), $1, $1, repeat('b', 64),
			'{"requested_channel_ids": ["UC_TEST"]}'::jsonb)
	`, old); err != nil {
		t.Fatalf("insert old absence slot: %v", err)
	}

	var deleted int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM public.delete_youtube_live_absence_slot_retention_batch($1, 1000)
	`, now.Add(-30*24*time.Hour)).Scan(&deleted); err != nil {
		t.Fatalf("retain with pending snapshot: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted %d slots while an old snapshot is pending", deleted)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE public.source_observation_queue
		SET status='PROCESSED', processed_at=$2
		WHERE observation_id=$1
	`, observationID, now); err != nil {
		t.Fatalf("complete old snapshot: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM public.delete_youtube_live_absence_slot_retention_batch($1, 1000)
	`, now.Add(-30*24*time.Hour)).Scan(&deleted); err != nil {
		t.Fatalf("delete after completion: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted %d slots after completion, want 1", deleted)
	}
}
