package sourceobservation

// consume 패키지의 통합 테스트에도 같은 fixture가 있다. 발행 테스트는 consume을 import하지 않으므로 필요한 fixture만 둔다.

import (
	"context"
	"encoding/json/jsontext"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

func publishMixedCollisionBatch(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	proof *contract.LeaseProof,
) (baseID int64, baseKey string, collision, independent *contract.Envelope, result PublishBatchResult) {
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
