package sourceobservation

import (
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestReplayProcessedObservationIsIdempotent(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	writer := NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil))
	consumer := NewConsumer(repo, writer, nil)

	proof = bootstrapCommunityWindow(ctx, t, pool, repo, consumer, proof)

	published, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := consumer.Consume(ctx, claimOptions()); err != nil {
		t.Fatalf("first consume: %v", err)
	}

	assertTableCount(t, pool, "youtube_notification_outbox", 1)

	for i := range 2 {
		replay, err := repo.RequestReplay(ctx, ReplayInput{
			ObservationID: published.Results[0].ObservationID,
			RequestedBy:   testReplayOperator,
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
	assertTableCount(t, pool, "source_observations", 2)
	assertTableCount(t, pool, "source_observation_replay_requests", 2)
}

func TestReplayUnsupportedSchemaIsRejectedWithAudit(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	observationID := publishOne(ctx, t, repo, &proof, "post-old")
	finalizeObservation(ctx, t, repo, observationID)

	unsupported := NewRepositoryWithContracts(pool, StaticSupportedContracts{}, InitialJobContracts(), nil)

	replay, err := unsupported.RequestReplay(ctx, ReplayInput{
		ObservationID: observationID,
		RequestedBy:   testReplayOperator,
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
