package sourceobservation

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func publishMixedCollisionBatch(
	ctx context.Context,
	t *testing.T,
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

	reactivateMixedLease(ctx, t, pool, proof)

	collision = communityEnvelope(t, proof, "post-collision")
	independent = independentCommunityEnvelope(t, proof)

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

func reactivateMixedLease(ctx context.Context, t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
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

	if err := jsonv2.Unmarshal(independent.Payload, &independentPayload); err != nil {
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

func assertMixedPersistence(ctx context.Context, t *testing.T, pool *pgxpool.Pool, baseID int64, independent *contract.Envelope) {
	t.Helper()
	assertMixedTableCount(ctx, t, pool, "source_observations", 2)
	assertMixedTableCount(ctx, t, pool, "source_observation_queue", 2)
	assertMixedTableCount(ctx, t, pool, "source_collection_checkpoints", 2)
	assertMixedTableCount(ctx, t, pool, "source_observation_collisions", 1)
	assertMixedCheckpoint(ctx, t, pool, independent)
	assertMixedQueue(ctx, t, pool, baseID, independent)
}

func assertMixedCheckpoint(ctx context.Context, t *testing.T, pool *pgxpool.Pool, independent *contract.Envelope) {
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

func assertMixedQueue(ctx context.Context, t *testing.T, pool *pgxpool.Pool, baseID int64, independent *contract.Envelope) {
	t.Helper()

	var (
		count           int
		firstID, lastID int64
	)

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

func assertMixedTableCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string, want int) {
	t.Helper()

	var count int

	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}

	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}
