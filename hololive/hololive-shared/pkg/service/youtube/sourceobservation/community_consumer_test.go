package sourceobservation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestConsumerIsolatesInvalidItemAndProcessesLaterBatchItem(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	first, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-bad")))
	if err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE source_observations SET payload = $1 WHERE id = $2`, []byte(`{"broken":true}`), first.Results[0].ObservationID); err != nil {
		t.Fatalf("corrupt payload: %v", err)
	}
	proof = advanceLease(t, pool, proof, time.Minute)
	second, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-good")))
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
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"))); err != nil {
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
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'COMMUNITY_POST', TRUE, 'old-post')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}
	published, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")))
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
	oldProof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_content_watermarks (channel_id, watermark_type, initialized, last_content_id)
		VALUES ($1, 'COMMUNITY_POST', TRUE, 'old-post')
	`, "UC_TEST"); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}
	old, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, oldProof, 1, contract.CompletenessComplete, "old-head")))
	if err != nil {
		t.Fatalf("publish old: %v", err)
	}
	newProof := advanceLease(t, pool, oldProof, time.Minute)
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, newProof, 1, contract.CompletenessComplete, "new-head"))); err != nil {
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

func (w failWriter) PersistTx(context.Context, dbx.Tx, community.Batch) error {
	return w.err
}

func (failWriter) AfterCommit(context.Context, community.Batch) {}
