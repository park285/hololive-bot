package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	publishkit "github.com/kapu/hololive-collector/testkit/sourceobservation"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestContentConsumerPositiveThenCompleteNegative(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")

	positive, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, testVideoID)))
	if err != nil {
		t.Fatalf("publish positive: %v", err)
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	negative, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete)))
	if err != nil {
		t.Fatalf("publish negative: %v", err)
	}

	consumer := NewConsumerWithGraces(repo, 0, 0)

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume positive: %v", err)
	}

	requireQueueStatus(ctx, t, pool, positive.Results[0].ObservationID, contract.StatusProcessed)
	requireQueueStatus(ctx, t, pool, negative.Results[0].ObservationID, contract.StatusPending)
	assertContentMissing(t, pool, testVideoID, false)

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume negative: %v", err)
	}

	requireQueueStatus(ctx, t, pool, negative.Results[0].ObservationID, contract.StatusProcessed)
	assertTableCount(t, pool, "youtube_videos", 1)
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	assertContentMissing(t, pool, testVideoID, true)
	assertContentWithdrawn(t, pool, testVideoID, false)
}

func TestContentConsumerCompleteNegativeThenPositive(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := NewRepository(pool)
	base := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	consumer := NewConsumerWithGraces(repo, 0, 0)

	late := moveContentLease(ctx, t, pool, &base, base.FenceEpoch+1, base.ScheduledFor.Add(time.Minute))

	negative, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &late, contract.CompletenessComplete)))
	if err != nil {
		t.Fatalf("publish negative: %v", err)
	}

	alignReceivedAtToSlot(ctx, t, pool, negative.Results[0].ObservationID)

	if consumeErr := consumer.Consume(ctx, contentClaimOptions()); consumeErr != nil {
		t.Fatalf("consume negative first: %v", consumeErr)
	}

	requireQueueStatus(ctx, t, pool, negative.Results[0].ObservationID, contract.StatusProcessed)

	early := moveContentLease(ctx, t, pool, &base, base.FenceEpoch+2, base.ScheduledFor)

	positive, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &early, contract.CompletenessComplete, testVideoID)))
	if err != nil {
		t.Fatalf("publish positive: %v", err)
	}

	alignReceivedAtToSlot(ctx, t, pool, positive.Results[0].ObservationID)

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume positive: %v", err)
	}

	requireQueueStatus(ctx, t, pool, positive.Results[0].ObservationID, contract.StatusProcessed)
	assertTableCount(t, pool, "youtube_videos", 1)
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	assertContentMissing(t, pool, testVideoID, true)
}

func alignReceivedAtToSlot(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE source_observations SET received_at = scheduled_for + INTERVAL '1 second' WHERE id = $1
	`, observationID); err != nil {
		t.Fatalf("align received_at for %d: %v", observationID, err)
	}
}

func TestContentConsumerInvalidItemDoesNotBlockLaterItem(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")

	first, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, "vid-bad")))
	if err != nil {
		t.Fatalf("publish first: %v", err)
	}

	if _, execErr := pool.Exec(ctx, `UPDATE source_observation_payloads SET payload = $1, payload_sha256 = decode($3, 'hex')
		WHERE id = (SELECT payload_id FROM source_observations WHERE id = $2)`, []byte(`{"broken":true}`), first.Results[0].ObservationID, contract.SHA256Hex([]byte(`{"broken":true}`))); execErr != nil {
		t.Fatalf("corrupt payload: %v", execErr)
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	second, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, "vid-good")))
	if err != nil {
		t.Fatalf("publish second: %v", err)
	}

	consumer := NewConsumerWithGraces(repo, 0, 0)

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume invalid: %v", err)
	}

	requireQueueStatus(ctx, t, pool, first.Results[0].ObservationID, contract.StatusDeadLetter)
	requireQueueStatus(ctx, t, pool, second.Results[0].ObservationID, contract.StatusPending)

	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume later: %v", err)
	}

	requireQueueStatus(ctx, t, pool, second.Results[0].ObservationID, contract.StatusProcessed)
}
