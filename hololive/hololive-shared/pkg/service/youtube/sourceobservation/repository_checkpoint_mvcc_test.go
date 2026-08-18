package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type checkpointTupleVersion struct {
	ctid          string
	xmin          string
	lastSuccessAt time.Time
	updatedAt     time.Time
}

func TestPublishBatchDuplicateDoesNotRewriteCheckpointTuple(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(
		t,
		ctx,
		pool,
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		"UC_TEST",
		"community_collect",
	)
	envelope := communityEnvelope(t, &proof, "post-1")
	repo := NewRepository(pool)

	first, err := repo.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if first.Results[0].Outcome != PublishInserted {
		t.Fatalf("first outcome = %s", first.Results[0].Outcome)
	}
	before := loadCheckpointTupleVersion(t, ctx, pool, envelope)

	reactivateLease(t, pool, &proof)
	second, err := repo.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish duplicate: %v", err)
	}
	if second.Results[0].Outcome != PublishDuplicate || second.Results[0].ObservationID != first.Results[0].ObservationID {
		t.Fatalf("duplicate result = %#v", second.Results[0])
	}
	after := loadCheckpointTupleVersion(t, ctx, pool, envelope)
	if after != before {
		t.Fatalf("duplicate publish rewrote checkpoint tuple: before=%+v after=%+v", before, after)
	}
}

func TestCheckpointUpsertNoopDoesNotRewriteTuple(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(
		t,
		ctx,
		pool,
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		"UC_TEST",
		"community_collect",
	)
	envelope := communityEnvelope(t, &proof, "post-1")
	input := publishInput(envelope)
	if _, err := NewRepository(pool).PublishBatch(ctx, input); err != nil {
		t.Fatalf("publish checkpoint seed: %v", err)
	}
	before := loadCheckpointTupleVersion(t, ctx, pool, envelope)

	checkpoint := input.Checkpoint.Entries[0]
	var cursor any
	if len(checkpoint.Cursor) != 0 {
		cursor = string(checkpoint.Cursor)
	}
	if _, err := pool.Exec(
		ctx,
		mustSQL("repository_checkpoint_upsert_0010_10.sql"),
		checkpoint.Provider,
		checkpoint.ObservationKind,
		checkpoint.SubjectKey,
		checkpoint.ScopeSHA256,
		checkpoint.ContractGeneration,
		checkpoint.LastObservationKey,
		checkpoint.LastEvidenceSHA256,
		checkpoint.LastScheduledFor,
		input.Checkpoint.CollectionLatency.Milliseconds(),
		checkpoint.Continuity,
		cursor,
	); err != nil {
		t.Fatalf("repeat exact checkpoint upsert: %v", err)
	}
	after := loadCheckpointTupleVersion(t, ctx, pool, envelope)
	if after != before {
		t.Fatalf("exact checkpoint upsert rewrote tuple: before=%+v after=%+v", before, after)
	}
}

func loadCheckpointTupleVersion(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	envelope *contract.Envelope,
) checkpointTupleVersion {
	t.Helper()

	var version checkpointTupleVersion
	if err := pool.QueryRow(ctx, `
		SELECT ctid::text, xmin::text, last_success_at, updated_at
		FROM source_collection_checkpoints
		WHERE provider = $1
		  AND observation_kind = $2
		  AND subject_key = $3
		  AND scope_sha256 = $4
	`, envelope.Provider, envelope.ObservationKind, envelope.SubjectKey, envelope.ScopeSHA256).Scan(
		&version.ctid,
		&version.xmin,
		&version.lastSuccessAt,
		&version.updatedAt,
	); err != nil {
		t.Fatalf("load checkpoint tuple version: %v", err)
	}
	return version
}
