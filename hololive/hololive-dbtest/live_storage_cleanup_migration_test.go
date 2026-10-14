package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiveStorageCleanupPreservesActiveAndCandidateEvidence(t *testing.T) {
	const migration = "305_live_storage_cleanup_and_lz4.sql"

	pool, dir := projectionMigrationPoolBefore(t, migration)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status)
		SELECT 'ended-' || lpad(n::text, 4, '0'), 'cleanup-channel', 'ENDED'
		FROM generate_series(1, 1001) n;
		INSERT INTO youtube_live_sessions(video_id, channel_id, status)
		VALUES ('active-live', 'cleanup-channel', 'LIVE'),
		       ('active-upcoming', 'cleanup-channel', 'UPCOMING'),
		       ('mismatch-live', 'cleanup-channel', 'LIVE'),
		       ('mismatch-upcoming', 'cleanup-channel', 'UPCOMING'),
		       ('candidate-ended', 'cleanup-channel', 'ENDED');
		INSERT INTO youtube_live_pending_ends
		(video_id, channel_id, kind, observation_id, effective_at, received_at, scheduled_for, negative_eligible, scope_covers)
		SELECT video_id, channel_id, 'EXPLICIT_END', 900, '2026-10-01T00:00:00Z',
		       '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z', true, true
		FROM youtube_live_sessions;
		INSERT INTO youtube_live_pending_ends
		(video_id, channel_id, kind, observation_id, effective_at, received_at, scheduled_for, negative_eligible, scope_covers)
		VALUES ('headless-pending', 'cleanup-channel', 'EXPLICIT_END', 901,
		        '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z', true, true);
		INSERT INTO youtube_live_reconciliation_heads(video_id, status, ignored_absence_scheduled_for)
		SELECT video_id, status, ARRAY['2026-10-01T00:00:00Z'::timestamptz, '2026-10-01T00:02:00Z'::timestamptz]
		FROM youtube_live_sessions;
		UPDATE youtube_live_reconciliation_heads SET status='ENDED'
		WHERE video_id IN ('mismatch-live', 'mismatch-upcoming');
		INSERT INTO youtube_live_reconciliation_heads(video_id, status, ignored_absence_scheduled_for)
		VALUES ('head-only-ended', 'ENDED', ARRAY['2026-10-01T00:00:00Z'::timestamptz]);
		UPDATE youtube_live_reconciliation_heads
		SET status='LIVE', end_candidate_kind='EXPLICIT_END', end_candidate_observation_id=900,
		    next_end_check_at='2026-10-15T00:00:00Z'
		WHERE video_id='candidate-ended';
	`)
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, migration))

		var cleared, preserved int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE status='ENDED' AND cardinality(ignored_absence_scheduled_for)=0),
			       count(*) FILTER (WHERE ignored_absence_scheduled_for=
			           ARRAY['2026-10-01T00:00:00Z'::timestamptz, '2026-10-01T00:02:00Z'::timestamptz])
			FROM youtube_live_reconciliation_heads`).Scan(&cleared, &preserved))
		require.Equal(t, 1002, cleared, "keyset 배치 경계 뒤 및 head만 남은 종료 이력도 비워야 합니다")
		require.Equal(t, 5, preserved, "canonical 활성 상태가 ENDED head보다 우선하며 종료 후보도 보존해야 합니다")

		var pendingIDs []string
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT array_agg(video_id ORDER BY video_id) FROM youtube_live_pending_ends`).Scan(&pendingIDs))
		require.Equal(t, []string{"active-live", "active-upcoming", "candidate-ended", "headless-pending", "mismatch-live", "mismatch-upcoming"}, pendingIDs)
	}
}
