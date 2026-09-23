package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestShortsWindowClaimsCannotOvertakePendingOrProcessingBaseline(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	consumer := newContentTestConsumer(pool, repo, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(t, repo, &proof, "known")

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	second := publishShortWindow(t, repo, &proof, "known", "new")
	_, err := pool.Exec(ctx, `UPDATE source_observation_queue SET available_at = NOW() + INTERVAL '1 hour' WHERE observation_id = $1`, first)
	require.NoError(t, err)

	blocked, err := repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Empty(t, blocked.Claims)

	var attempts int

	require.NoError(t, pool.QueryRow(ctx, `SELECT attempt_count FROM source_observation_queue WHERE observation_id = $1`, second).Scan(&attempts))
	require.Zero(t, attempts)

	_, err = pool.Exec(ctx, `UPDATE source_observation_queue SET available_at = NOW() - INTERVAL '1 second' WHERE observation_id = $1`, first)
	require.NoError(t, err)

	batch, err := repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, first, batch.Claims[0].ObservationID)

	blocked, err = repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Empty(t, blocked.Claims)
	expireObservationClaim(t, pool, first)

	reclaimed, err := repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, reclaimed.Claims, 1)
	require.Equal(t, first, reclaimed.Claims[0].ObservationID)
	require.NoError(t, consumer.ConsumeClaim(ctx, reclaimed.Claims[0].Claim(reclaimed.ConsumerName)))
	assertTableCount(t, pool, "youtube_notification_outbox", 0)

	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
	assertTableCount(t, pool, "youtube_videos", 2)

	var completeAt *time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT earliest_complete_effective_at FROM youtube_content_channel_heads WHERE channel_id = $1 AND observation_kind = 'shorts_list'`, testChannelID).Scan(&completeAt))
	require.Nil(t, completeAt)

	replay, err := repo.RequestReplay(ctx, ReplayInput{ObservationID: first, RequestedBy: testReplayOperator, Reason: "baseline must stay silent"})
	require.NoError(t, err)
	require.True(t, replay.Applied)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
}

func TestShortsWindowLegacyEmptyPartialWatermarkInitializesSilently(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(ctx, `INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized) VALUES ($1, 'SHORT', TRUE)`, testChannelID)
	require.NoError(t, err)

	repo := NewRepository(pool)
	consumer := newContentTestConsumer(pool, repo, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	publishShortWindow(t, repo, &proof, "known-a", "known-b")
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertTableCount(t, pool, "youtube_videos", 2)
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
	assertTableCount(t, pool, "youtube_community_shorts_alarm_states", 0)

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishShortWindow(t, repo, &proof, "known-a", "known-b", "new")
	require.NoError(t, newContentTestConsumer(pool, repo, 0).Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
	assertTableCount(t, pool, "youtube_videos", 3)
}

func TestShortsWindowBaselineWithExistingVideoKind(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(ctx, `INSERT INTO youtube_videos (video_id, channel_id, title, is_short, first_seen_at, last_seen_at) VALUES ('known', $1, 'known', FALSE, NOW(), NOW())`, testChannelID)
	require.NoError(t, err)

	repo := NewRepository(pool)
	consumer := newContentTestConsumer(pool, repo, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	publishShortWindow(t, repo, &proof, "known")
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertTableCount(t, pool, "youtube_notification_outbox", 0)

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishShortWindow(t, repo, &proof, "known", "new")
	require.NoError(t, newContentTestConsumer(pool, repo, 0).Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
	assertTableCount(t, pool, "youtube_community_shorts_alarm_states", 1)

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishShortWindow(t, repo, &proof, "known", "new")
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
	assertTableCount(t, pool, "youtube_community_shorts_alarm_states", 1)

	var isShort bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT is_short FROM youtube_videos WHERE video_id = 'known'`).Scan(&isShort))
	require.False(t, isShort)
}

func TestShortsWindowExistingPartialCatalogDoesNotBackfill(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, is_short, first_seen_at, last_seen_at)
		SELECT 'old-' || n, $1, 'old-' || n, TRUE, TIMESTAMPTZ '2026-08-01 00:00:00Z', TIMESTAMPTZ '2026-08-01 00:00:00Z'
		FROM generate_series(1, 49) AS n
	`, testChannelID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id) VALUES ($1, 'SHORT', TRUE, 'old-1')`, testChannelID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_content_channel_heads (channel_id, observation_kind) VALUES ($1, 'shorts_list')`, testChannelID)
	require.NoError(t, err)

	repo := NewRepository(pool)
	consumer := newContentTestConsumer(pool, repo, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(t, repo, &proof, "old-1", "old-2", "new")
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
	assertTableCount(t, pool, "youtube_videos", 50)
	assertTableCount(t, pool, "youtube_community_shorts_alarm_states", 1)

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishShortWindow(t, repo, &proof, "new", "old-49", "old-1")
	require.NoError(t, newContentTestConsumer(pool, repo, 0).Consume(ctx, contentClaimOptions()))

	replay, err := repo.RequestReplay(ctx, ReplayInput{ObservationID: first, RequestedBy: testReplayOperator, Reason: "existing catalog must not backfill"})
	require.NoError(t, err)
	require.True(t, replay.Applied)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
}

func TestShortsWindowPersistenceFailureRollsBackNotificationAndCatalog(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	consumer := newContentTestConsumer(pool, repo, 0)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	publishShortWindow(t, repo, &proof, "known")
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	next := publishShortWindow(t, repo, &proof, "new", "known")
	writer := failShortWindowWriter{CanonicalWriter: NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))}
	err := NewConsumer(repo, writer, nil).Consume(ctx, contentClaimOptions())
	require.ErrorContains(t, err, "injected failure after video persistence")
	assertTableCount(t, pool, "youtube_videos", 1)
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
	assertTableCount(t, pool, "youtube_community_shorts_alarm_states", 0)

	expireObservationClaim(t, pool, next)
	require.NoError(t, consumer.Consume(ctx, contentClaimOptions()))
	assertShortWindowOutboxes(t, pool, "new")
	assertTableCount(t, pool, "youtube_videos", 2)
}

func TestShortsWindowOrderingIsChannelScopedAndTerminalDoesNotBlock(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(t, repo, &proof, "known")

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	second := publishShortWindow(t, repo, &proof, "new")
	_, err := pool.Exec(ctx, `UPDATE source_observation_queue SET available_at = NOW() + INTERVAL '1 hour' WHERE observation_id = $1`, first)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET status = 'RETIRED' WHERE status = 'CURRENT'`)
	require.NoError(t, err)

	const otherChannel = "UC_OTHER"

	otherProof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, otherChannel, "youtubejs_content")
	envelope := shortsListEnvelope(t, &otherProof, contract.CompletenessPartial, "other")

	var payload contract.ShortsListV1

	require.NoError(t, jsonv2.Unmarshal(envelope.Payload, &payload))

	payload.ChannelID = otherChannel
	payload.Coverage.ChannelID = otherChannel
	payload.Videos[0].ChannelID = otherChannel
	envelope.SubjectKey = otherChannel
	envelope.Payload, err = contract.MarshalPayloadV1(payload)
	require.NoError(t, err)

	prepared, err := contract.PrepareEnvelope(*envelope)
	require.NoError(t, err)

	other, err := repo.PublishBatch(ctx, publishInput(&prepared))
	require.NoError(t, err)

	batch, err := repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, other.Results[0].ObservationID, batch.Claims[0].ObservationID)
	require.NoError(t, newContentTestConsumer(pool, repo, 0).ConsumeClaim(ctx, batch.Claims[0].Claim(batch.ConsumerName)))

	_, err = pool.Exec(ctx, `UPDATE source_observation_queue SET attempt_count = $2, available_at = NOW() - INTERVAL '1 second' WHERE observation_id = $1`, first, MaxAttempts)
	require.NoError(t, err)

	_, err = repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)

	var status string

	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM source_observation_queue WHERE observation_id = $1`, first).Scan(&status))
	require.Equal(t, "DEAD_LETTER", status)

	batch, err = repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, second, batch.Claims[0].ObservationID)
}

func TestShortsWindowConcurrentClaimersChooseOnlyOldest(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(t, repo, &proof, "known")

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)
	publishShortWindow(t, repo, &proof, "new", "known")

	type result struct {
		batch ClaimedBatch
		err   error
	}

	results := make(chan result, 2)
	start := make(chan struct{})

	for range 2 {
		go func() {
			<-start

			batch, err := repo.ClaimBatch(ctx, contentClaimOptions())
			results <- result{batch: batch, err: err}
		}()
	}

	close(start)

	claims := make([]ClaimWork, 0, 2)

	for range 2 {
		got := <-results
		require.NoError(t, got.err)

		claims = append(claims, got.batch.Claims...)
	}

	require.Len(t, claims, 1)
	require.Equal(t, first, claims[0].ObservationID)
}

func TestShortsWindowExpiredReplayCannotBlockEligibleObservation(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(t, repo, &proof, "expired")

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	second := publishShortWindow(t, repo, &proof, "eligible")
	_, err := pool.Exec(ctx, `UPDATE source_observations SET received_at = NOW() - INTERVAL '2 hours' WHERE id = $1`, first)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE source_observation_queue SET available_at = NOW() + INTERVAL '1 hour' WHERE observation_id = $1`, first)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO source_observation_replay_epoch (cutoff_received_at, activated_by, reason) VALUES (NOW() - INTERVAL '1 hour', 'test', 'exclude pre-epoch predecessor')`)
	require.NoError(t, err)

	batch, err := repo.ClaimBatch(ctx, contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, second, batch.Claims[0].ObservationID)

	var attempts int

	require.NoError(t, pool.QueryRow(ctx, `SELECT attempt_count FROM source_observation_queue WHERE observation_id = $1`, first).Scan(&attempts))
	require.Zero(t, attempts)
}

type failShortWindowWriter struct {
	CanonicalWriter
}

func (w failShortWindowWriter) PersistVideosTx(ctx context.Context, tx dbx.Tx, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, tracking []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error {
	if err := w.CanonicalWriter.PersistVideosTx(ctx, tx, videos, notifications, tracking, watermark); err != nil {
		return fmt.Errorf("persist videos before injected failure: %w", err)
	}

	return errors.New("injected failure after video persistence")
}

func publishShortWindow(t *testing.T, repo *Repository, proof *contract.LeaseProof, ids ...string) int64 {
	t.Helper()

	envelope := shortsListEnvelope(t, proof, contract.CompletenessPartial, ids...)

	envelope.Continuity = contract.ContinuityGapUnresolved

	prepared, err := contract.PrepareEnvelope(*envelope)
	require.NoError(t, err)

	published, err := repo.PublishBatch(t.Context(), publishInput(&prepared))
	require.NoError(t, err)
	require.Len(t, published.Results, 1)

	return published.Results[0].ObservationID
}

func assertShortWindowOutboxes(t *testing.T, pool *pgxpool.Pool, ids ...string) {
	t.Helper()

	var got []string

	rows, err := pool.Query(t.Context(), `SELECT payload::jsonb->>'video_id' FROM youtube_notification_outbox WHERE kind = 'NEW_SHORT' ORDER BY payload::jsonb->>'video_id'`)
	require.NoError(t, err)

	defer rows.Close()

	for rows.Next() {
		var id string

		require.NoError(t, rows.Scan(&id))

		got = append(got, id)
	}

	require.NoError(t, rows.Err())
	require.Equal(t, ids, got)
}
