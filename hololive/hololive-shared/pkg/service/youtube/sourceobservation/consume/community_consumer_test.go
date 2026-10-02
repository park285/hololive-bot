package consume

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/youtube/community"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

func TestPublishBatchMixedCollisionStillQueuesAndPublishesIndependentObservation(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := newTestRepository(pool)
	proof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	baseID, baseKey, collision, independent, result := publishMixedCollisionBatch(ctx, t, pool, repo, &proof)

	assertMixedPublishResult(t, baseID, baseKey, collision, independent, result)
	observationtest.AssertMixedPersistence(ctx, t, pool, baseID, independent)
	seedMixedCommunityWatermark(ctx, t, pool)
	consumeMixedCommunityBatch(ctx, t, pool, repo)
	assertMixedOutbox(ctx, t, pool)
}

func publishMixedCollisionBatch(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *testRepository,
	proof *contract.LeaseProof,
) (baseID int64, baseKey string, collision, independent *contract.Envelope, result sourceobservation.PublishBatchResult) {
	t.Helper()

	base := observationtest.CommunityEnvelope(t, proof, "post-base")

	first, err := repo.PublishBatch(ctx, publishInput(base))
	if err != nil {
		t.Fatalf("publish base: %v", err)
	}

	observationtest.ReactivateLease(ctx, t, pool, proof)

	collision = observationtest.CommunityEnvelope(t, proof, "post-collision")
	independent = observationtest.IndependentCommunityEnvelope(t, proof)

	mixed := publishInput(collision)

	mixed.Observations = append(mixed.Observations, *independent)

	independentCheckpoint := checkpointForEnvelope(independent)

	independentCheckpoint.Cursor = jsontext.Value(`{"page":2}`)
	mixed.Checkpoint.Entries = append(mixed.Checkpoint.Entries, independentCheckpoint)

	result, err = repo.PublishBatch(ctx, mixed)
	if err != nil {
		t.Fatalf("publish mixed batch: %v", err)
	}

	return first.Results[0].ObservationID, base.ObservationKey, collision, independent, result
}

func assertMixedPublishResult(t *testing.T, baseID int64, baseKey string, collision, independent *contract.Envelope, result sourceobservation.PublishBatchResult) {
	t.Helper()

	if collision.ObservationKey != baseKey {
		t.Fatalf("collision identity = %s, base identity = %s", collision.ObservationKey, baseKey)
	}

	if collision.ObservationKey == independent.ObservationKey {
		t.Fatal("independent observation must have a distinct identity")
	}

	if len(result.Results) != 2 {
		t.Fatalf("mixed results = %#v, want two rows", result.Results)
	}

	if got := result.Results[0]; got.Outcome != sourceobservation.PublishCollision || got.ObservationID != baseID {
		t.Fatalf("collision result = %#v, want existing observation %d", got, baseID)
	}

	if got := result.Results[1]; got.Outcome != sourceobservation.PublishInserted || got.ObservationID <= 0 || got.ObservationID == baseID {
		t.Fatalf("independent result = %#v, want a new observation", got)
	}
}

func seedMixedCommunityWatermark(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'COMMUNITY_POST', TRUE, 'old-post')
	`, testChannelID); err != nil {
		t.Fatalf("seed community watermark: %v", err)
	}
}

func consumeMixedCommunityBatch(ctx context.Context, t *testing.T, pool *pgxpool.Pool, repo *testRepository) {
	t.Helper()

	writer := NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	if err := NewConsumer(repo, writer, nil).Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("consume mixed queue: %v", err)
	}
}

func assertMixedOutbox(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var outboxRows int

	if err := pool.QueryRow(ctx, `SELECT count(id) FROM youtube_notification_outbox`).Scan(&outboxRows); err != nil {
		t.Fatalf("count youtube_notification_outbox: %v", err)
	}

	if outboxRows != 1 {
		t.Fatalf("youtube_notification_outbox count = %d, want 1", outboxRows)
	}

	assertMixedInsertedOutbox(ctx, t, pool)
	assertMixedCollisionOutbox(ctx, t, pool)
}

func assertMixedInsertedOutbox(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var independentOutboxCount int

	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM youtube_notification_outbox
		WHERE content_id = 'community:post-independent'
	`).Scan(&independentOutboxCount); err != nil {
		t.Fatalf("count inserted-row outbox: %v", err)
	}

	if independentOutboxCount != 1 {
		t.Fatalf("inserted-row outbox count = %d, want 1", independentOutboxCount)
	}

	var baselineOutboxCount int

	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM youtube_notification_outbox WHERE content_id = 'community:post-base'
	`).Scan(&baselineOutboxCount); err != nil {
		t.Fatalf("count baseline-row outbox: %v", err)
	}

	if baselineOutboxCount != 0 {
		t.Fatalf("baseline-row outbox count = %d, want 0", baselineOutboxCount)
	}
}

func assertMixedCollisionOutbox(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var collisionOutboxCount int

	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM youtube_notification_outbox WHERE content_id = 'community:post-collision'
	`).Scan(&collisionOutboxCount); err != nil {
		t.Fatalf("count collision-row outbox: %v", err)
	}

	if collisionOutboxCount != 0 {
		t.Fatalf("collision-row outbox count = %d, want 0", collisionOutboxCount)
	}
}

func TestConsumerIsolatesInvalidItemAndProcessesLaterBatchItem(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := newTestRepository(pool)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	first, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "post-bad")))
	if err != nil {
		t.Fatalf("publish first: %v", err)
	}

	if _, execErr := pool.Exec(ctx, `UPDATE source_observation_payloads SET payload = $1, payload_sha256 = decode($3, 'hex')
		WHERE id = (SELECT payload_id FROM source_observations WHERE id = $2)`, []byte(`{"broken":true}`), first.Results[0].ObservationID, contract.SHA256Hex([]byte(`{"broken":true}`))); execErr != nil {
		t.Fatalf("corrupt payload: %v", execErr)
	}

	proof = observationtest.AdvanceLease(t.Context(), t, pool, &proof, time.Minute)

	second, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "post-good")))
	if err != nil {
		t.Fatalf("publish second: %v", err)
	}

	if len(second.Results) != 1 || second.Results[0].Outcome != sourceobservation.PublishInserted {
		t.Fatalf("second publish = %#v", second)
	}

	writer := NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	if err := NewConsumer(repo, writer, nil).Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}

	var firstStatus, secondStatus string

	if err := pool.QueryRow(ctx, `SELECT status FROM source_observation_queue WHERE observation_id = $1`, first.Results[0].ObservationID).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}

	if firstStatus != string(contract.StatusDeadLetter) {
		t.Fatalf("invalid item status = %s, want DEAD_LETTER", firstStatus)
	}

	if err := pool.QueryRow(ctx, `SELECT status FROM source_observation_queue WHERE observation_id = $1`, second.Results[0].ObservationID).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}

	if secondStatus != string(contract.StatusProcessed) {
		t.Fatalf("later item status = %s, want PROCESSED", secondStatus)
	}
}

func TestConsumerTransactionFailureRollsBackCanonicalAndProcessedState(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := newTestRepository(pool)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	if _, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}

	err := NewConsumer(repo, failWriter{err: errors.New("canonical write failed")}, nil).Consume(ctx, claimOptions())
	if err == nil {
		t.Fatal("expected consume error")
	}

	assertTableCount(t, pool, "youtube_community_posts", 0)
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
	assertTableCount(t, pool, "source_observation_applications", 0)

	var status string

	if err := pool.QueryRow(ctx, `SELECT status FROM source_observation_queue`).Scan(&status); err != nil {
		t.Fatal(err)
	}

	if status == string(contract.StatusProcessed) {
		t.Fatal("failed consume must not mark the queue processed")
	}
}

func TestConsumerReplayDoesNotDuplicateNotificationIntent(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := newTestRepository(pool)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	writer := NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	consumer := NewConsumer(repo, writer, nil)

	proof = bootstrapCommunityWindow(ctx, t, pool, repo, consumer, proof)

	published, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "post-1")))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	if consumeErr := consumer.Consume(ctx, claimOptions()); consumeErr != nil {
		t.Fatalf("first consume: %v", consumeErr)
	}

	assertTableCount(t, pool, "youtube_notification_outbox", 1)

	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID,
		RequestedBy:   testReplayOperator,
		Reason:        "duplicate notification guard",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: %#v err=%v", replay, err)
	}

	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("replay consume: %v", err)
	}

	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	assertTableCount(t, pool, "source_observations", 2)
}

func setQueueAvailability(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64, offset, action string) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE source_observation_queue
		SET available_at = NOW() + $2::interval
		WHERE observation_id = $1
	`, observationID, offset); err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

func requireApplicationDecision(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64, want string) {
	t.Helper()

	var decision string

	if err := pool.QueryRow(ctx, `
		SELECT decision FROM source_observation_applications
		WHERE observation_id = $1 AND entity_kind = 'community_subject_head'
	`, observationID).Scan(&decision); err != nil {
		t.Fatal(err)
	}

	if decision != want {
		t.Fatalf("older observation decision = %s, want %s", decision, want)
	}
}

func requireCommunityWatermark(ctx context.Context, t *testing.T, pool *pgxpool.Pool, want string) {
	t.Helper()

	var watermark string

	if err := pool.QueryRow(ctx, `
		SELECT last_content_id FROM youtube_content_watermarks
		WHERE channel_id = $1 AND watermark_type = 'COMMUNITY_POST'
	`, testChannelID).Scan(&watermark); err != nil {
		t.Fatal(err)
	}

	if watermark != want {
		t.Fatalf("watermark = %s, want %s", watermark, want)
	}
}

func TestConsumerDoesNotRegressCanonicalStateWhenOlderObservationFinishesLast(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := newTestRepository(pool)
	oldProof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	seedMixedCommunityWatermark(ctx, t, pool)

	old, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &oldProof, "old-head")))
	if err != nil {
		t.Fatalf("publish old: %v", err)
	}

	newProof := observationtest.AdvanceLease(ctx, t, pool, &oldProof, time.Minute)
	if _, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &newProof, "new-head"))); err != nil {
		t.Fatalf("publish new: %v", err)
	}

	oldestID := old.Results[0].ObservationID

	setQueueAvailability(ctx, t, pool, oldestID, "1 hour", "defer oldest")

	writer := NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	consumer := NewConsumer(repo, writer, nil)

	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("consume newer: %v", err)
	}

	setQueueAvailability(ctx, t, pool, oldestID, "-1 second", "make oldest due")

	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("consume older: %v", err)
	}

	requireApplicationDecision(ctx, t, pool, oldestID, "STALE_SKIPPED")
	requireCommunityWatermark(ctx, t, pool, "community:new-head")
}

type failWriter struct {
	err error
}

func (w failWriter) PersistTx(context.Context, dbx.Tx, *community.Batch) error {
	return w.err
}

func (failWriter) AfterCommit(context.Context, *community.Batch) {}

func (w failWriter) PersistVideosTx(
	context.Context,
	dbx.Tx,
	[]*domain.YouTubeVideo,
	[]*domain.YouTubeNotificationOutbox,
	[]*domain.YouTubeContentAlarmTracking,
	*domain.YouTubeContentWatermark,
) error {
	return w.err
}

func (failWriter) AfterCommitVideos(context.Context, []*domain.YouTubeContentAlarmTracking) {}
