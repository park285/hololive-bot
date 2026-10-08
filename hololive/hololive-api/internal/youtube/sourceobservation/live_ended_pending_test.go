package sourceobservation

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

type storedPendingRow struct {
	observationID int64
	xmin          string
}

func storedPendingRows(t *testing.T, pool *pgxpool.Pool) map[string]storedPendingRow {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT video_id, observation_id, xmin::text FROM youtube_live_pending_ends`)
	require.NoError(t, err)

	defer rows.Close()

	stored := map[string]storedPendingRow{}

	for rows.Next() {
		var (
			videoID string
			row     storedPendingRow
		)

		require.NoError(t, rows.Scan(&videoID, &row.observationID, &row.xmin))

		stored[videoID] = row
	}

	require.NoError(t, rows.Err())

	return stored
}

// pendingRowXmax는 행 잠금 흔적을 읽는다. FOR UPDATE와 ON CONFLICT DO UPDATE는 값이 같아도 잠근
// 트랜잭션 ID를 xmax에 남기므로, 값이 그대로여도 xmax가 바뀌면 그 행을 잠근 것이다.
func pendingRowXmax(t *testing.T, pool *pgxpool.Pool, videoID string) string {
	t.Helper()

	var xmax string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT xmax::text FROM youtube_live_pending_ends WHERE video_id=$1`, videoID).Scan(&xmax))

	return xmax
}

// YouTube.js streams 탭은 이미 ENDED인 세션도 매번 ENDED로 싣는다. 저장된 ENDED 세션의 기존 pending(D2)은
// 같은 tuple로 남고 행 잠금도 받지 않으며, pending이 없던 ENDED 세션에는 행이 생기지 않는다. 세션 없는 영상의
// 종료는 새 관측으로 갱신한다.
func TestLiveConsumerFreezesPendingEndsOfEndedSessions(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()
	ended := proof.ScheduledFor.Add(-2 * time.Hour)
	stale := proof.ScheduledFor.Add(-time.Hour)
	seed := func(statement string, args ...any) {
		t.Helper()

		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err)
	}

	seed(`
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, started_at, ended_at, last_seen_at,
		    live_first_seen_at, lifecycle_origin, status_observed_at)
		VALUES ('ended-pending', $2, 'ENDED', 'p', $1, $1, $1, $1, 'observed', $1),
		       ('ended-bare', $2, 'ENDED', 'b', $1, $1, $1, $1, 'observed', $1)`, ended, testChannelID)
	seed(`
		INSERT INTO youtube_live_reconciliation_heads (video_id, status, last_live_positive_at, last_live_positive_seen_at,
		    last_end_evidence_at, ended_at, end_reason)
		VALUES ('ended-pending', 'ENDED', $1, $1, $1, $1, 'EXPLICIT_END'),
		       ('ended-bare', 'ENDED', $1, $1, $1, $1, 'EXPLICIT_END')`, ended)
	seed(`
		INSERT INTO youtube_live_pending_ends (video_id, channel_id, kind, observation_id, effective_at, received_at,
		    scheduled_for, ended_at, negative_eligible, scope_covers)
		VALUES ('ended-pending', $2, 'EXPLICIT_END', 900, $1, $1, $1, NULL, true, true),
		       ('orphan-end', $2, 'EXPLICIT_END', 901, $1, $1, $1, NULL, true, true)`, stale, testChannelID)

	before := storedPendingRows(t, pool)
	frozenXmax := pendingRowXmax(t, pool, "ended-pending")
	observationID := publishConsumeLiveFromProvider(ctx, t, publishkit.NewPublisher(pool), consumer, &proof,
		contract.ProviderYouTubeJS, testChannelID,
		liveSession("ended-pending", testStatusEnded), liveSession("ended-bare", testStatusEnded),
		liveSession("orphan-end", testStatusEnded))

	requireQueueStatus(ctx, t, pool, observationID, contract.StatusProcessed)

	after := storedPendingRows(t, pool)

	require.Equal(t, before["ended-pending"], after["ended-pending"], "ENDED session pending was rewritten")
	require.Equal(t, frozenXmax, pendingRowXmax(t, pool, "ended-pending"), "frozen ENDED session pending was locked")
	require.NotContains(t, after, "ended-bare", "ENDED session without pending gained a row")
	require.Equal(t, observationID, after["orphan-end"].observationID, "pending of a video without session was not refreshed")
	require.Len(t, after, 2)
}

// session은 ENDED인데 head에 아직 due가 아닌 종료 후보가 남은 행(ended/head 불일치)도 반복 종료로 pending의
// 관측 ID를 바꾸지 않는다. 바꾸면 head 후보 FK가 commit에서 깨져 관측을 처리하지 못한다. 후보가 남은 세션은
// 동결 대상이 아니므로 pending을 계속 읽고 잠근다.
func TestLiveConsumerKeepsCandidatePendingOfEndedSession(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()
	ended := proof.ScheduledFor.Add(-2 * time.Hour)
	seed := func(statement string, args ...any) {
		t.Helper()

		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err)
	}

	seed(`
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, started_at, ended_at, last_seen_at,
		    live_first_seen_at, lifecycle_origin, status_observed_at)
		VALUES ('ended-cand', $2, 'ENDED', 'c', $1, $1, $1, $1, 'observed', $1)`, ended, testChannelID)
	seed(`
		INSERT INTO youtube_live_pending_ends (video_id, channel_id, kind, observation_id, effective_at, received_at,
		    scheduled_for, ended_at, negative_eligible, scope_covers)
		VALUES ('ended-cand', $2, 'EXPLICIT_END', 900, $1, $1, $1, NULL, true, true)`, ended, testChannelID)
	seed(`
		INSERT INTO youtube_live_reconciliation_heads (video_id, status, last_live_positive_at, last_live_positive_seen_at,
		    end_candidate_kind, end_candidate_observation_id, next_end_check_at)
		VALUES ('ended-cand', 'LIVE', $1, $1, 'EXPLICIT_END', 900, NOW() + interval '100 days')`, ended)

	before := storedPendingRows(t, pool)
	candidateXmax := pendingRowXmax(t, pool, "ended-cand")
	observationID := publishConsumeLiveFromProvider(ctx, t, publishkit.NewPublisher(pool), consumer, &proof,
		contract.ProviderYouTubeJS, testChannelID, liveSession("ended-cand", testStatusEnded))

	requireQueueStatus(ctx, t, pool, observationID, contract.StatusProcessed)
	require.Equal(t, before, storedPendingRows(t, pool), "ENDED session pending was rewritten")
	require.NotEqual(t, candidateXmax, pendingRowXmax(t, pool, "ended-cand"), "candidate ENDED session pending was not loaded")
}
