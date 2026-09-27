package sourceobservation

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// 같은 시각의 content positive가 저장 값과 다른 값을 가져오면 기존 값을 유지하고, 충돌은 공용 reconcile 충돌 SQL(0061)로
// youtube_video·KEEP_EXISTING 행 하나로 남는다. 0042에서 상수였던 entity_kind·decision이 이제 인자이므로 인자 순서가
// 어긋나면 이 행의 식별 열과 effective_at이 달라진다. 같은 관측을 replay해도 행은 늘지 않는다.
func TestContentConsumerRecordsEqualTimeConflictOnce(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := t.Context()
	storedSHA := strings.Repeat("ab", 32)

	coverage, err := contract.MarshalPayloadV1(contract.ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10, Exhausted: true})
	require.NoError(t, err)

	// 관측과 같은 effective_at에 다른 값 digest를 가진 clock을 두어 같은 시각의 값 충돌을 만든다.
	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, first_seen_at, last_seen_at)
		VALUES ('vid-x', $1, 'stored title', $2, $2)
	`, testChannelID, proof.ScheduledFor)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_content_evidence_clocks (
			video_id, first_positive_effective_at, last_positive_effective_at, last_positive_received_at,
			last_positive_value_sha256, last_positive_scope_sha256, last_positive_coverage
		) VALUES ('vid-x', $1, $1, $1, $2, $2, $3)
	`, proof.ScheduledFor, storedSHA, coverage)
	require.NoError(t, err)

	published, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessPartial, "vid-x")))
	require.NoError(t, err)
	require.Len(t, published.Results, 1)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

	var title string

	require.NoError(t, pool.QueryRow(ctx, `SELECT title FROM youtube_videos WHERE video_id = 'vid-x'`).Scan(&title))
	require.Equal(t, "stored title", title, "equal-time conflict must keep the existing value")

	var (
		observationID                              int64
		entityKind, entityKey, fieldName, decision string
		existingSHA, attemptedSHA                  string
		effectiveAt                                time.Time
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT observation_id, entity_kind, entity_key, field_name, decision, effective_at,
		       existing_value_sha256, attempted_value_sha256
		FROM source_reconciliation_conflicts
	`).Scan(&observationID, &entityKind, &entityKey, &fieldName, &decision, &effectiveAt, &existingSHA, &attemptedSHA))
	require.Equal(t, published.Results[0].ObservationID, observationID)
	require.Equal(t, "youtube_video", entityKind)
	require.Equal(t, "vid-x", entityKey)
	require.Equal(t, "content", fieldName)
	require.Equal(t, "KEEP_EXISTING", decision)
	require.True(t, effectiveAt.Equal(proof.ScheduledFor), "effective_at = %s, want %s", effectiveAt, proof.ScheduledFor)
	require.Equal(t, storedSHA, existingSHA)
	require.NotEqual(t, storedSHA, attemptedSHA)

	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID,
		RequestedBy:   testReplayOperator,
		Reason:        "content conflict replay",
	})
	require.NoError(t, err)
	require.True(t, replay.Applied)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

	var (
		status      string
		replayCount int
	)

	// replay가 관측을 다시 처리해 충돌 기록을 한 번 더 시도했음을 확인한 뒤 행 수를 본다.
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, replay_count FROM source_observation_queue WHERE observation_id = $1
	`, published.Results[0].ObservationID).Scan(&status, &replayCount))
	require.Equal(t, "PROCESSED", status)
	require.Equal(t, 1, replayCount)
	assertTableCount(t, pool, "source_reconciliation_conflicts", 1)
}
