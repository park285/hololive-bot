package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestContentConsumerDoesNotRewriteAbsentCatalogRow(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)
	lastSeen := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedCatalogVideoWithClock(t, pool, "vid-keep", 42, lastSeen)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindVideoList, "UC_TEST", "youtubejs_content")
	consumer := newContentTestConsumer(pool, repo, 0)
	if _, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete, "vid-new"))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}
	gotCount, gotSeen := lockVideoCatalog(t, pool, "vid-keep")
	if gotCount != 42 {
		t.Fatalf("view_count = %d, want 42", gotCount)
	}
	if !gotSeen.Equal(lastSeen) {
		t.Fatalf("last_seen_at = %s, want %s", gotSeen, lastSeen)
	}
	assertContentMissing(t, pool, "vid-keep", true)
	assertContentWithdrawn(t, pool, "vid-keep", false)
}

func TestContentConsumerDoesNotRearmFailedShort(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedShortsWatermark(t, pool)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindShortsList, "UC_TEST", "youtubejs_content")
	consumer := newContentTestConsumer(pool, repo, 0)
	if _, err := repo.PublishBatch(ctx, publishInput(shortsListEnvelope(t, &proof, 1, contract.CompletenessComplete, "vid-s"))); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	contentID := polling.NormalizeContentID(domain.OutboxKindNewShort, "vid-s")
	if _, err := pool.Exec(ctx, `
		UPDATE youtube_notification_outbox SET status = $1 WHERE kind = $2 AND content_id = $3
	`, domain.OutboxStatusFailed, domain.OutboxKindNewShort, contentID); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	proof = advanceLease(t, ctx, pool, &proof, time.Minute)
	if _, err := repo.PublishBatch(ctx, publishInput(shortsListEnvelope(t, &proof, 1, contract.CompletenessComplete, "vid-s"))); err != nil {
		t.Fatalf("publish later: %v", err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("later consume: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM youtube_notification_outbox WHERE kind = $1 AND content_id = $2
	`, domain.OutboxKindNewShort, contentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.OutboxStatusFailed) {
		t.Fatalf("status = %s, want FAILED", status)
	}
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
}

func TestContentConsumerPersistPartialThenPositive(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := context.Background()
	proof = publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessPartial)
	publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete, "vid-a")
	assertPersistBoundary(t, pool, false, false)
}

func TestContentConsumerPersistLatePositiveClearsMissing(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := context.Background()
	proof = publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete, "vid-a")
	proof = publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete)
	publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete, "vid-a")
	assertPersistBoundary(t, pool, false, false)
}

func TestContentConsumerPersistNarrowScopeNegative(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := context.Background()
	proof = publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete, "vid-a")
	after := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	proof = advanceLease(t, ctx, pool, &proof, time.Minute)
	payload, err := contract.MarshalPayloadV1(contract.VideoListV1{
		ChannelID: "UC_TEST",
		Videos:    []contract.VideoListItemV1{},
		Coverage: contract.ChannelListCoverageV1{
			ChannelID: "UC_TEST", MaxResults: 10, Exhausted: true,
			Filters: contract.VideoListFiltersV1{PublishedAfter: &after},
		},
	})
	if err != nil {
		t.Fatalf("marshal narrow: %v", err)
	}
	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindVideoList, SubjectKey: "UC_TEST",
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: proof,
	})
	if err != nil {
		t.Fatalf("prepare narrow: %v", err)
	}
	if _, err := repo.PublishBatch(ctx, publishInput(&envelope)); err != nil {
		t.Fatalf("publish narrow: %v", err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume narrow: %v", err)
	}
	assertPersistBoundary(t, pool, false, false)
}

func TestContentConsumerPersistOneNegativeRecordsMissing(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := context.Background()
	proof = publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete, "vid-a")
	publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete)
	assertPersistBoundary(t, pool, true, false)
}

func TestContentConsumerPersistTwoNegativesWithGraceWithdraws(t *testing.T) {
	pool, repo, consumer, proof := startContentPersistGrace(t, time.Hour)
	ctx := context.Background()
	proof = publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete, "vid-a")
	proof = publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete)
	var lastPositive time.Time
	if err := pool.QueryRow(ctx, `
		SELECT last_positive_received_at FROM youtube_content_evidence_clocks WHERE video_id = 'vid-a'
	`).Scan(&lastPositive); err != nil {
		t.Fatal(err)
	}
	proof = advanceLease(t, ctx, pool, &proof, time.Minute)
	published, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, &proof, contract.CompletenessComplete)))
	if err != nil {
		t.Fatalf("publish second negative: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE source_observations SET received_at = $1 WHERE id = $2`, lastPositive.Add(time.Hour+time.Second), published.Results[0].ObservationID); err != nil {
		t.Fatalf("set received_at: %v", err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume second negative: %v", err)
	}
	assertPersistBoundary(t, pool, true, true)
}

func TestContentConsumerPersistReplayedNegativeDoesNotIncrement(t *testing.T) {
	pool, repo, consumer, proof := startContentPersist(t)
	ctx := context.Background()
	proof = publishConsumeVideos(t, ctx, pool, repo, consumer, &proof, contract.CompletenessComplete, "vid-a")
	nextProof := advanceLease(t, ctx, pool, &proof, time.Minute)
	published, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, &nextProof, contract.CompletenessComplete)))
	if err != nil {
		t.Fatalf("publish negative: %v", err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume negative: %v", err)
	}
	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID,
		RequestedBy:   "test-operator",
		Reason:        "replayed negative count",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: %#v err=%v", replay, err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("replay consume: %v", err)
	}
	assertPersistBoundary(t, pool, true, false)
	if slots := contentConsecutiveSlots(t, pool, "vid-a"); slots != 1 {
		t.Fatalf("consecutive = %d, want 1", slots)
	}
}

func newContentTestConsumer(pool *pgxpool.Pool, repo *Repository, grace time.Duration) *Consumer {
	return NewConsumerWithAbsenceGrace(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, grace)
}

func startContentPersist(t *testing.T) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof) {
	t.Helper()
	return startContentPersistGrace(t, 0)
}

func startContentPersistGrace(t *testing.T, grace time.Duration) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof) {
	t.Helper()
	pool := dbtest.NewPool(t)
	seedContentWatermark(t, pool)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindVideoList, "UC_TEST", "youtubejs_content")
	return pool, repo, newContentTestConsumer(pool, repo, grace), proof
}

func publishConsumeVideos(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	completeness contract.Completeness,
	videoIDs ...string,
) contract.LeaseProof {
	t.Helper()
	if _, err := repo.PublishBatch(ctx, publishInput(videoListEnvelope(t, proof, completeness, videoIDs...))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := consumer.Consume(ctx, contentClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}
	return advanceLease(t, ctx, pool, proof, time.Minute)
}

func lockVideoCatalog(t *testing.T, pool *pgxpool.Pool, videoID string) (int64, time.Time) {
	t.Helper()
	var viewCount int64
	var lastSeen time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT view_count, last_seen_at FROM youtube_videos WHERE video_id = $1 FOR UPDATE
	`, videoID).Scan(&viewCount, &lastSeen); err != nil {
		t.Fatalf("lock youtube_videos %s: %v", videoID, err)
	}
	return viewCount, lastSeen
}

func assertPersistBoundary(t *testing.T, pool *pgxpool.Pool, missing, withdrawn bool) {
	t.Helper()
	lockVideoCatalog(t, pool, "vid-a")
	assertContentMissing(t, pool, "vid-a", missing)
	assertContentWithdrawn(t, pool, "vid-a", withdrawn)
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
}

func contentConsecutiveSlots(t *testing.T, pool *pgxpool.Pool, videoID string) int {
	t.Helper()
	var slots int
	if err := pool.QueryRow(context.Background(), `
		SELECT consecutive_absence_slots FROM youtube_content_evidence_clocks WHERE video_id = $1
	`, videoID).Scan(&slots); err != nil {
		t.Fatalf("load consecutive slots: %v", err)
	}
	return slots
}
