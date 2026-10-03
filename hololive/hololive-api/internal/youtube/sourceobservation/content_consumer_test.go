package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func TestContentConsumerPositiveThenCompleteNegative(t *testing.T) {
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

	consumer := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil)

	// 같은 채널·종류 목록은 한 tick에 선두 하나만 claim하므로, 앞선 등장 관측이 끝나기 전 부재 관측은 대기한다.
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
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := NewRepository(pool)
	base := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")
	consumer := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil)

	// 늦은 slot의 complete 부재가 먼저 처리된 뒤 이른 slot의 등장 관측이 처리되는 역순을 만든다.
	// queue는 활성 같은 채널 목록끼리 추월시키지 않으므로, 이른 관측을 늦은 관측 처리 뒤에 발행한다.
	// 수신 시각은 각 slot 직후로 맞춰 등장 관측이 먼저 수신됐고 처리만 늦은 상황의 grace 판정을 유지한다.
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

// alignReceivedAtToSlot은 관측 수신 시각을 slot 직후로 맞춰 발행 순서와 무관하게 grace 판정이 slot 순서를 따르게 한다.
func alignReceivedAtToSlot(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64) {
	t.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE source_observations SET received_at = scheduled_for + INTERVAL '1 second' WHERE id = $1
	`, observationID); err != nil {
		t.Fatalf("align received_at for %d: %v", observationID, err)
	}
}

func TestContentConsumerReplayDoesNotDuplicateNotification(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)

	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindVideoList, testChannelID, "youtubejs_content")

	published, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, testVideoID)))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	consumer := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil)
	if consumeErr := consumer.Consume(ctx, contentClaimOptions()); consumeErr != nil {
		t.Fatalf("first consume: %v", consumeErr)
	}

	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID,
		RequestedBy:   testReplayOperator,
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

	consumer := NewConsumer(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil)

	// 첫 tick은 같은 채널 선두인 손상 관측만 claim해 DEAD_LETTER로 보내고, 후속 관측은 선행 관측이 끝날 때까지 대기한다.
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume invalid: %v", err)
	}

	requireQueueStatus(ctx, t, pool, first.Results[0].ObservationID, contract.StatusDeadLetter)
	requireQueueStatus(ctx, t, pool, second.Results[0].ObservationID, contract.StatusPending)

	// DEAD_LETTER는 더 이상 활성 선행 관측이 아니므로 다음 tick에 후속 관측이 처리된다.
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume later: %v", err)
	}

	requireQueueStatus(ctx, t, pool, second.Results[0].ObservationID, contract.StatusProcessed)
}
