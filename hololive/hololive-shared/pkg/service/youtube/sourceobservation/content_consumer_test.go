package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestContentConsumerPositiveThenCompleteNegative(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, "UC_TEST", "youtubejs_content")
	if _, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, proof, 1, contract.CompletenessComplete, "vid-a"))); err != nil {
		t.Fatalf("publish positive: %v", err)
	}
	proof = advanceLease(t, pool, proof, time.Minute)
	if _, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, proof, 1, contract.CompletenessComplete))); err != nil {
		t.Fatalf("publish negative: %v", err)
	}
	if err := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil).Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}
	assertTableCount(t, pool, "youtube_videos", 1)
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	assertContentMissing(t, pool, "vid-a", true)
	assertContentWithdrawn(t, pool, "vid-a", false)
}

func TestContentConsumerCompleteNegativeThenPositive(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)
	repo := NewRepository(pool)
	oldProof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, "UC_TEST", "youtubejs_content")
	old, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, oldProof, 1, contract.CompletenessComplete, "vid-a")))
	if err != nil {
		t.Fatalf("publish positive: %v", err)
	}
	newProof := advanceLease(t, pool, oldProof, time.Minute)
	if _, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, newProof, 1, contract.CompletenessComplete))); err != nil {
		t.Fatalf("publish negative: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE source_observation_queue SET available_at = NOW() + INTERVAL '1 hour' WHERE observation_id = $1`, old.Results[0].ObservationID); err != nil {
		t.Fatalf("defer positive: %v", err)
	}
	consumer := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil)
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume negative first: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE source_observation_queue SET available_at = NOW() - INTERVAL '1 second' WHERE observation_id = $1`, old.Results[0].ObservationID); err != nil {
		t.Fatalf("make positive due: %v", err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume positive: %v", err)
	}
	assertTableCount(t, pool, "youtube_videos", 1)
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	assertContentMissing(t, pool, "vid-a", true)
}

func TestContentConsumerReplayDoesNotDuplicateNotification(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, "UC_TEST", "youtubejs_content")
	published, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, proof, 1, contract.CompletenessComplete, "vid-a")))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	consumer := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil)
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID,
		RequestedBy:   "test-operator",
		Reason:        "duplicate notification guard",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: %#v err=%v", replay, err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("replay consume: %v", err)
	}
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
}

func TestContentConsumerInvalidItemDoesNotBlockLaterItem(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, "UC_TEST", "youtubejs_content")
	first, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, proof, 1, contract.CompletenessComplete, "vid-bad")))
	if err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE source_observations SET payload = $1 WHERE id = $2`, []byte(`{"broken":true}`), first.Results[0].ObservationID); err != nil {
		t.Fatalf("corrupt payload: %v", err)
	}
	proof = advanceLease(t, pool, proof, time.Minute)
	if _, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, proof, 1, contract.CompletenessComplete, "vid-good"))); err != nil {
		t.Fatalf("publish second: %v", err)
	}
	if err := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil).Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}
	var firstStatus, secondStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM source_observation_queue WHERE observation_id = $1`, first.Results[0].ObservationID).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != string(contract.StatusDeadLetter) {
		t.Fatalf("invalid item status = %s, want DEAD_LETTER", firstStatus)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM source_observation_queue ORDER BY observation_id DESC LIMIT 1`).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}
	if secondStatus != string(contract.StatusProcessed) {
		t.Fatalf("later item status = %s, want PROCESSED", secondStatus)
	}
}
