package sourceobservation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestPublishBatchMixedCollisionStillQueuesAndPublishesIndependentObservation(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	baseID, baseKey, collision, independent, result := publishMixedCollisionBatch(t, ctx, pool, repo, &proof)

	assertMixedPublishResult(t, baseID, baseKey, collision, independent, result)
	assertMixedPersistence(t, ctx, pool, baseID, independent)
	seedMixedCommunityWatermark(t, ctx, pool)
	consumeMixedCommunityBatch(t, ctx, pool, repo)
	assertMixedOutbox(t, ctx, pool)
}

func publishMixedCollisionBatch(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *Repository,
	proof *contract.LeaseProof,
) (baseID int64, baseKey string, collision, independent *contract.Envelope, result PublishBatchResult) {
	t.Helper()
	base := communityEnvelope(t, proof, "post-base")
	first, err := repo.PublishBatch(ctx, publishInput(base))
	if err != nil {
		t.Fatalf("publish base: %v", err)
	}
	reactivateMixedLease(t, ctx, pool, proof)

	collision = communityEnvelope(t, proof, "post-collision")
	independent = independentCommunityEnvelope(t, proof)
	mixed := publishInput(collision)
	mixed.Observations = append(mixed.Observations, *independent)
	independentCheckpoint := checkpointForEnvelope(independent)
	independentCheckpoint.Cursor = json.RawMessage(`{"page":2}`)
	mixed.Checkpoint.Entries = append(mixed.Checkpoint.Entries, independentCheckpoint)
	result, err = repo.PublishBatch(ctx, mixed)
	if err != nil {
		t.Fatalf("publish mixed batch: %v", err)
	}
	return first.Results[0].ObservationID, base.ObservationKey, collision, independent, result
}

func reactivateMixedLease(t *testing.T, ctx context.Context, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
		    retry_not_before = NULL, last_error_code = NULL
		WHERE job_key = $1
	`, proof.JobKey, proof.OwnerInstance); err != nil {
		t.Fatalf("reactivate lease: %v", err)
	}
}

func independentCommunityEnvelope(t *testing.T, proof *contract.LeaseProof) *contract.Envelope {
	t.Helper()
	independent := communityEnvelope(t, proof, "post-independent")
	var independentPayload contract.CommunityPayloadV1
	if err := json.Unmarshal(independent.Payload, &independentPayload); err != nil {
		t.Fatalf("decode independent payload: %v", err)
	}
	independentPayload.Coverage.MaxResults = 20
	encodedPayload, err := contract.MarshalPayloadV1(independentPayload)
	if err != nil {
		t.Fatalf("marshal independent payload: %v", err)
	}
	independent.Payload = encodedPayload
	preparedIndependent, err := contract.PrepareEnvelope(*independent)
	if err != nil {
		t.Fatalf("prepare independent envelope: %v", err)
	}
	independent = &preparedIndependent
	return independent
}

func assertMixedPublishResult(t *testing.T, baseID int64, baseKey string, collision, independent *contract.Envelope, result PublishBatchResult) {
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
	if got := result.Results[0]; got.Outcome != PublishCollision || got.ObservationID != baseID {
		t.Fatalf("collision result = %#v, want existing observation %d", got, baseID)
	}
	if got := result.Results[1]; got.Outcome != PublishInserted || got.ObservationID <= 0 || got.ObservationID == baseID {
		t.Fatalf("independent result = %#v, want a new observation", got)
	}
}

func assertMixedPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, baseID int64, independent *contract.Envelope) {
	t.Helper()
	assertMixedTableCount(t, ctx, pool, "source_observations", 2)
	assertMixedTableCount(t, ctx, pool, "source_observation_queue", 2)
	assertMixedTableCount(t, ctx, pool, "source_collection_checkpoints", 2)
	assertMixedTableCount(t, ctx, pool, "source_observation_collisions", 1)
	assertMixedCheckpoint(t, ctx, pool, independent)
	assertMixedQueue(t, ctx, pool, baseID, independent)
}

func assertMixedCheckpoint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, independent *contract.Envelope) {
	t.Helper()
	var checkpointObservationKey string
	if err := pool.QueryRow(ctx, `
		SELECT last_observation_key
		FROM source_collection_checkpoints
		WHERE provider = $1 AND observation_kind = $2 AND subject_key = $3 AND scope_sha256 = $4
	`, independent.Provider, independent.ObservationKind, independent.SubjectKey, independent.ScopeSHA256).Scan(&checkpointObservationKey); err != nil {
		t.Fatalf("load independent checkpoint: %v", err)
	}
	if checkpointObservationKey != independent.ObservationKey {
		t.Fatalf("independent checkpoint key = %s, want %s", checkpointObservationKey, independent.ObservationKey)
	}
}

func assertMixedQueue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, baseID int64, independent *contract.Envelope) {
	t.Helper()
	var count int
	var firstID, lastID int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*), min(observation_id), max(observation_id)
		FROM source_observation_queue
	`).Scan(&count, &firstID, &lastID); err != nil {
		t.Fatalf("load mixed queue: %v", err)
	}
	var independentID int64
	if err := pool.QueryRow(ctx, `
		SELECT id FROM source_observations WHERE observation_key = $1
	`, independent.ObservationKey).Scan(&independentID); err != nil {
		t.Fatalf("load independent observation: %v", err)
	}
	if count != 2 || firstID != baseID || lastID != independentID {
		t.Fatalf("mixed queue IDs = count:%d first:%d last:%d, want count:2 first:%d last:%d", count, firstID, lastID, baseID, independentID)
	}
}

func assertMixedTableCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}

func seedMixedCommunityWatermark(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'COMMUNITY_POST', TRUE, 'old-post')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed community watermark: %v", err)
	}
}

func consumeMixedCommunityBatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository) {
	t.Helper()
	writer := NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	if err := NewConsumer(repo, writer, nil).Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("consume mixed queue: %v", err)
	}
}

func assertMixedOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	assertMixedTableCount(t, ctx, pool, "youtube_notification_outbox", 2)
	assertMixedInsertedOutbox(t, ctx, pool)
	assertMixedCollisionOutbox(t, ctx, pool)
}

func assertMixedInsertedOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var independentOutboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM youtube_notification_outbox
		WHERE content_id IN ('community:post-base', 'community:post-independent')
	`).Scan(&independentOutboxCount); err != nil {
		t.Fatalf("count inserted-row outbox: %v", err)
	}
	if independentOutboxCount != 2 {
		t.Fatalf("inserted-row outbox count = %d, want 2", independentOutboxCount)
	}
}

func assertMixedCollisionOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	first, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-bad")))
	if err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE source_observations SET payload = $1 WHERE id = $2`, []byte(`{"broken":true}`), first.Results[0].ObservationID); err != nil {
		t.Fatalf("corrupt payload: %v", err)
	}
	proof = advanceLease(t, context.Background(), pool, &proof, time.Minute)
	second, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-good")))
	if err != nil {
		t.Fatalf("publish second: %v", err)
	}
	if len(second.Results) != 1 || second.Results[0].Outcome != PublishInserted {
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1"))); err != nil {
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'COMMUNITY_POST', TRUE, 'old-post')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}
	published, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	writer := NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	consumer := NewConsumer(repo, writer, nil)
	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: published.Results[0].ObservationID,
		RequestedBy:   "test-operator",
		Reason:        "duplicate notification guard",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: %#v err=%v", replay, err)
	}
	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("replay consume: %v", err)
	}
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	assertTableCount(t, pool, "source_observations", 1)
}

func TestConsumerDoesNotRegressCanonicalStateWhenOlderObservationFinishesLast(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	oldProof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'COMMUNITY_POST', TRUE, 'old-post')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}
	old, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &oldProof, "old-head")))
	if err != nil {
		t.Fatalf("publish old: %v", err)
	}
	newProof := advanceLease(t, context.Background(), pool, &oldProof, time.Minute)
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &newProof, "new-head"))); err != nil {
		t.Fatalf("publish new: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE source_observation_queue
		SET available_at = NOW() + INTERVAL '1 hour'
		WHERE observation_id = $1
	`, old.Results[0].ObservationID); err != nil {
		t.Fatalf("defer oldest: %v", err)
	}
	writer := NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	consumer := NewConsumer(repo, writer, nil)
	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("consume newer: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE source_observation_queue
		SET available_at = NOW() - INTERVAL '1 second'
		WHERE observation_id = $1
	`, old.Results[0].ObservationID); err != nil {
		t.Fatalf("make oldest due: %v", err)
	}
	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("consume older: %v", err)
	}
	var decision string
	if err := pool.QueryRow(ctx, `
		SELECT decision FROM source_observation_applications
		WHERE observation_id = $1 AND entity_kind = 'community_subject_head'
	`, old.Results[0].ObservationID).Scan(&decision); err != nil {
		t.Fatal(err)
	}
	if decision != "STALE_SKIPPED" {
		t.Fatalf("older observation decision = %s, want STALE_SKIPPED", decision)
	}
	var watermark string
	if err := pool.QueryRow(ctx, `
		SELECT last_content_id FROM youtube_content_watermarks
		WHERE channel_id = $1 AND watermark_type = 'COMMUNITY_POST'
	`, "UC_TEST").Scan(&watermark); err != nil {
		t.Fatal(err)
	}
	if watermark != "community:new-head" {
		t.Fatalf("watermark = %s, want community:new-head", watermark)
	}
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
