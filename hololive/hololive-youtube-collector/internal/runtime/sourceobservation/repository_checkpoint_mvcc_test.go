package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type checkpointTupleVersion struct {
	ctid          string
	xmin          string
	lastSuccessAt time.Time
	updatedAt     time.Time
}

func TestPublishBatchDuplicateDoesNotRewriteCheckpointTuple(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t,
		pool,
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		testChannelID,
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

	before := loadCheckpointTupleVersion(ctx, t, pool, envelope)

	reactivateLease(t, pool, &proof)

	second, err := repo.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish duplicate: %v", err)
	}

	if second.Results[0].Outcome != PublishDuplicate || second.Results[0].ObservationID != first.Results[0].ObservationID {
		t.Fatalf("duplicate result = %#v", second.Results[0])
	}

	after := loadCheckpointTupleVersion(ctx, t, pool, envelope)
	if after != before {
		t.Fatalf("duplicate publish rewrote checkpoint tuple: before=%+v after=%+v", before, after)
	}
}

func TestPublishBatchDuplicateUpdatesChangedCheckpointMetadata(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t,
		pool,
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		testChannelID,
		"community_collect",
	)
	envelope := communityEnvelope(t, &proof, "post-1")
	input := publishInput(envelope)
	repo := NewRepository(pool)

	first, err := repo.PublishBatch(ctx, input)
	if err != nil {
		t.Fatalf("publish checkpoint seed: %v", err)
	}

	before := loadCheckpointTupleVersion(ctx, t, pool, envelope)

	input.Checkpoint.CollectionLatency += time.Millisecond

	reactivateLease(t, pool, &proof)

	second, err := repo.PublishBatch(ctx, input)
	if err != nil {
		t.Fatalf("publish changed checkpoint: %v", err)
	}

	if second.Results[0].Outcome != PublishDuplicate || second.Results[0].ObservationID != first.Results[0].ObservationID {
		t.Fatalf("changed checkpoint duplicate result = %#v", second.Results[0])
	}

	after := loadCheckpointTupleVersion(ctx, t, pool, envelope)
	if after == before {
		t.Fatalf("changed checkpoint did not rewrite tuple: before=%+v after=%+v", before, after)
	}

	var latency int64

	if err := pool.QueryRow(ctx, `
		SELECT collection_latency_ms
		FROM source_collection_checkpoints
		WHERE provider = $1
		  AND observation_kind = $2
		  AND subject_key = $3
		  AND scope_sha256 = $4
	`, envelope.Provider, envelope.ObservationKind, envelope.SubjectKey, envelope.ScopeSHA256).Scan(&latency); err != nil {
		t.Fatalf("load checkpoint latency: %v", err)
	}

	if latency != input.Checkpoint.CollectionLatency.Milliseconds() {
		t.Fatalf("collection_latency_ms = %d, want %d", latency, input.Checkpoint.CollectionLatency.Milliseconds())
	}
}

func TestPublishBatchDuplicateClearsCheckpointErrorState(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(ctx, t,
		pool,
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		testChannelID,
		"community_collect",
	)
	envelope := communityEnvelope(t, &proof, "post-1")
	input := publishInput(envelope)
	repo := NewRepository(pool)

	first, err := repo.PublishBatch(ctx, input)
	if err != nil {
		t.Fatalf("publish checkpoint seed: %v", err)
	}

	if _, execErr := pool.Exec(ctx, `
		UPDATE source_collection_checkpoints
		SET last_error_code = 'timeout', last_error_at = NOW()
		WHERE provider = $1
		  AND observation_kind = $2
		  AND subject_key = $3
		  AND scope_sha256 = $4
	`, envelope.Provider, envelope.ObservationKind, envelope.SubjectKey, envelope.ScopeSHA256); execErr != nil {
		t.Fatalf("seed checkpoint error state: %v", execErr)
	}

	before := loadCheckpointTupleVersion(ctx, t, pool, envelope)

	reactivateLease(t, pool, &proof)

	second, err := repo.PublishBatch(ctx, input)
	if err != nil {
		t.Fatalf("publish checkpoint recovery: %v", err)
	}

	if second.Results[0].Outcome != PublishDuplicate || second.Results[0].ObservationID != first.Results[0].ObservationID {
		t.Fatalf("checkpoint recovery duplicate result = %#v", second.Results[0])
	}

	after := loadCheckpointTupleVersion(ctx, t, pool, envelope)
	if after == before {
		t.Fatalf("checkpoint recovery did not rewrite tuple: before=%+v after=%+v", before, after)
	}

	var errorCodeCleared, errorAtCleared bool

	if err := pool.QueryRow(ctx, `
		SELECT last_error_code IS NULL, last_error_at IS NULL
		FROM source_collection_checkpoints
		WHERE provider = $1
		  AND observation_kind = $2
		  AND subject_key = $3
		  AND scope_sha256 = $4
	`, envelope.Provider, envelope.ObservationKind, envelope.SubjectKey, envelope.ScopeSHA256).Scan(
		&errorCodeCleared,
		&errorAtCleared,
	); err != nil {
		t.Fatalf("load checkpoint error state: %v", err)
	}

	if !errorCodeCleared || !errorAtCleared {
		t.Fatalf("checkpoint error state not cleared: code=%t at=%t", errorCodeCleared, errorAtCleared)
	}
}

func loadCheckpointTupleVersion(
	ctx context.Context,
	t *testing.T,
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
