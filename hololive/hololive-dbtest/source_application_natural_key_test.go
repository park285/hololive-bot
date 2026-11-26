package dbtest

import (
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func finalizeApplications(t *testing.T, pool *pgxpool.Pool, observationID int64, kinds, keys, decisions []string) pgconn.CommandTag {
	t.Helper()

	query, err := os.ReadFile("../hololive-api/internal/youtube/sourceobservation/queries/write_finalize_applications.sql")
	require.NoError(t, err)

	tag, err := pool.Exec(t.Context(), string(query), observationID, kinds, keys, decisions)
	require.NoError(t, err)

	return tag
}

func finalizeLiveSessionApplication(t *testing.T, pool *pgxpool.Pool, observationID int64, videoID string) {
	t.Helper()
	finalizeApplications(t, pool, observationID, []string{"youtube_live_session"}, []string{videoID}, []string{"APPLIED"})
}

func liveSessionApplicationHead(t *testing.T, pool *pgxpool.Pool, videoID string) int64 {
	t.Helper()

	var observationID int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT coalesce(max(observation_id), 0)
		FROM youtube_live_session_application_heads WHERE video_id=$1`, videoID).Scan(&observationID))

	return observationID
}

func TestSourceApplicationNaturalKeyPreservesFinalizeAndCascade(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	first := insertRetentionObservation(t, pool, communityPageKind, base, "natural-first")
	second := insertRetentionObservation(t, pool, communityPageKind, base, "natural-second")

	for _, id := range []int64{first, second} {
		tag := finalizeApplications(t, pool, id,
			[]string{"community_window", "youtube_live_session", "community_window"},
			[]string{"same", "same", "other"}, []string{"APPLIED", "APPLIED", "APPLIED"})
		require.EqualValues(t, 3, tag.RowsAffected())

		tag = finalizeApplications(t, pool, id,
			[]string{"community_window"}, []string{"same"}, []string{"CANONICALIZED"})
		require.Zero(t, tag.RowsAffected(), "자연 키가 같으면 다른 결정도 덮어쓰지 않는다")
	}

	require.Equal(t, second, liveSessionApplicationHead(t, pool, "same"))
	finalizeLiveSessionApplication(t, pool, first, "same")
	require.Equal(t, second, liveSessionApplicationHead(t, pool, "same"), "늦게 도착한 이전 원본은 세션 최신 적용을 되돌리지 않는다")

	var decision string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT d.name
		FROM source_observation_applications a JOIN source_observation_decisions d ON d.id=a.decision_id
		WHERE a.observation_id=$1 AND a.entity_kind_id=source_observation_entity_kind_id('community_window')
		  AND a.entity_key='same'`, first).Scan(&decision))
	require.Equal(t, "APPLIED", decision)

	_, err := pool.Exec(t.Context(), `DELETE FROM source_observations WHERE id=$1`, first)
	require.NoError(t, err)

	var firstCount, secondCount int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		count(*) FILTER (WHERE observation_id=$1),count(*) FILTER (WHERE observation_id=$2)
		FROM source_observation_applications`, first, second).Scan(&firstCount, &secondCount))
	require.Zero(t, firstCount, "원본 삭제는 해당 감사만 함께 삭제한다")
	require.Equal(t, 3, secondCount)

	_, err = pool.Exec(t.Context(), `DELETE FROM source_observations WHERE id=$1`, second)
	require.NoError(t, err)
	require.Zero(t, liveSessionApplicationHead(t, pool, "same"), "세션 최신 적용은 원본과 함께 삭제된다")
}
