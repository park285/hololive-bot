package sourceobservation

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	publishkit "github.com/kapu/hololive-collector/testkit/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestContentConsumerRecordsEqualTimeConflictOnce(t *testing.T) {
	pool, _, consumer, proof := startContentPersist(t)
	ctx := t.Context()
	storedSHA := strings.Repeat("ab", 32)

	coverage, err := contract.MarshalPayloadV1(contract.ChannelListCoverageV1{ChannelID: testChannelID, MaxResults: 10, Exhausted: true})
	require.NoError(t, err)

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

	published, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessPartial, "vid-x")))
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

	assertTableCount(t, pool, "source_reconciliation_conflicts", 1)
}
