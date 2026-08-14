package sourceobservation

import (
	"context"
	"testing"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestReplayProcessedObservationIsIdempotent(t *testing.T) {
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
	for i := 0; i < 2; i++ {
		replay, err := repo.RequestReplay(ctx, ReplayInput{
			ObservationID: published.Results[0].ObservationID,
			RequestedBy:   "test-operator",
			Reason:        "idempotent replay",
		})
		if err != nil || !replay.Applied {
			t.Fatalf("request replay %d: %#v err=%v", i, replay, err)
		}
		if err := consumer.Consume(ctx, claimOptions()); err != nil {
			t.Fatalf("replay consume %d: %v", i, err)
		}
	}
	assertTableCount(t, pool, "youtube_notification_outbox", 1)
	assertTableCount(t, pool, "source_observations", 1)
	assertTableCount(t, pool, "source_observation_replay_requests", 2)
}

func TestReplayUnsupportedSchemaIsRejectedWithAudit(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	observationID := publishOne(t, ctx, repo, proof, "post-old")
	finalizeObservation(t, ctx, repo, observationID)

	unsupported := NewRepositoryWithContracts(pool, StaticSupportedContracts{}, InitialJobContracts(), nil)
	replay, err := unsupported.RequestReplay(ctx, ReplayInput{
		ObservationID: observationID,
		RequestedBy:   "test-operator",
		Reason:        "old schema",
	})
	if err != nil {
		t.Fatalf("request replay: %v", err)
	}
	if replay.Applied || replay.RejectionCode != "unsupported_contract" {
		t.Fatalf("replay = %#v, want unsupported_contract", replay)
	}
	var status, code string
	if err := pool.QueryRow(ctx, `
		SELECT status, rejection_code
		FROM source_observation_replay_requests
		WHERE id = $1
	`, replay.RequestID).Scan(&status, &code); err != nil {
		t.Fatalf("load audit: %v", err)
	}
	if status != "REJECTED" || code != "unsupported_contract" {
		t.Fatalf("audit status=%s code=%s", status, code)
	}
	assertQueueStatus(t, pool, observationID, string(contract.StatusProcessed))
}
