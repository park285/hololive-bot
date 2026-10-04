package sourceobservation

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func TestPublishBatchDuplicateKeepsOneEvidenceAndQueueRow(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	envelope := communityEnvelope(t, &proof, "post-1")
	repo := NewRepository(pool)

	first, err := repo.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish first: %v", err)
	}

	if first.Results[0].Outcome != PublishInserted {
		t.Fatalf("first outcome = %s", first.Results[0].Outcome)
	}

	reactivateLease(t, pool, &proof)

	second, err := repo.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish duplicate: %v", err)
	}

	if second.Results[0].Outcome != PublishDuplicate || second.Results[0].ObservationID != first.Results[0].ObservationID {
		t.Fatalf("duplicate result = %#v", second.Results[0])
	}

	assertTableCount(t, pool, "source_observations", 1)
	assertTableCount(t, pool, "source_observation_queue", 1)
}

func TestPublishBatchSemanticCollisionAuditsWithoutMutatingEvidenceQueueOrCheckpoint(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	base := communityEnvelope(t, &proof, "post-1")
	repo := NewRepository(pool)

	if _, err := repo.PublishBatch(ctx, publishInput(base)); err != nil {
		t.Fatalf("publish base: %v", err)
	}

	for _, mutate := range []func(contract.Envelope) contract.Envelope{
		func(_ contract.Envelope) contract.Envelope {
			return *communityEnvelope(t, &proof, "post-2")
		},
		func(envelope contract.Envelope) contract.Envelope {
			envelope.Completeness = contract.CompletenessPartial

			prepared, err := contract.PrepareEnvelope(envelope)
			if err != nil {
				t.Fatalf("prepare completeness collision: %v", err)
			}

			return prepared
		},
	} {
		reactivateLease(t, pool, &proof)

		collision := mutate(*base)

		result, err := repo.PublishBatch(ctx, publishInput(&collision))
		if err != nil {
			t.Fatalf("publish collision: %v", err)
		}

		if result.Results[0].Outcome != PublishCollision {
			t.Fatalf("collision outcome = %s", result.Results[0].Outcome)
		}
	}

	assertTableCount(t, pool, "source_observations", 1)
	assertTableCount(t, pool, "source_observation_queue", 1)
	assertTableCount(t, pool, "source_collection_checkpoints", 1)
	assertTableCount(t, pool, "source_observation_collisions", 2)
}

func TestPublishBatchSamePayloadNextScheduledSlotCreatesTwoObservations(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	firstProof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	repo := NewRepository(pool)
	first := communityEnvelope(t, &firstProof, "post-1")

	if _, err := repo.PublishBatch(ctx, publishInput(first)); err != nil {
		t.Fatalf("publish first slot: %v", err)
	}

	secondProof := advanceLease(t.Context(), t, pool, &firstProof)
	second := communityEnvelope(t, &secondProof, "post-1")

	if first.PayloadSHA256 != second.PayloadSHA256 || first.ObservationKey == second.ObservationKey {
		t.Fatalf("payload/identity mismatch across slots: first=%s second=%s", first.ObservationKey, second.ObservationKey)
	}

	if _, err := repo.PublishBatch(ctx, publishInput(second)); err != nil {
		t.Fatalf("publish second slot: %v", err)
	}

	assertTableCount(t, pool, "source_observations", 2)
	assertTableCount(t, pool, "source_observation_queue", 2)
}

func TestHistoricalViewerPublishEqualValueNextWindowCreatesTwoObservations(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	firstProof := seedPublishLease(t.Context(), t, pool, contract.ProviderHolodex, contract.KindViewerSample, "video-1", "holodex_live")
	repo := historicalViewerPublisher(t, pool)
	first := viewerEnvelope(t, &firstProof, 1, 100)

	if _, err := repo.PublishBatch(ctx, publishInput(first)); err != nil {
		t.Fatalf("publish first sample: %v", err)
	}

	secondProof := advanceLease(t.Context(), t, pool, &firstProof)
	second := viewerEnvelope(t, &secondProof, 1, 100)

	if _, err := repo.PublishBatch(ctx, publishInput(second)); err != nil {
		t.Fatalf("publish second sample: %v", err)
	}

	assertTableCount(t, pool, "source_observations", 2)
}

func TestPublishBatchRejectsUnrelatedCheckpointWithoutWrites(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	insertPublishTarget(ctx, t, pool, proof.ProjectionGeneration, "UC_OTHER", contract.KindCommunityPage, true)

	oldEvidence := strings.Repeat("c", 64)
	if _, err := pool.Exec(ctx, `
		INSERT INTO source_collection_checkpoints (
			provider, observation_kind, subject_key, scope_sha256,
			contract_generation, last_observation_key, last_evidence_sha256,
			last_scheduled_for, last_success_at, collection_latency_ms,
			continuity, cursor
		) VALUES ('youtubejs', 'community_page', 'UC_OTHER', $1, 1, 'old-observation', $2,
		          $3, NOW(), 1000, 'CONTIGUOUS', '{"page":0}'::jsonb)
	`, strings.Repeat("b", 64), oldEvidence, proof.ScheduledFor); err != nil {
		t.Fatalf("seed unrelated checkpoint: %v", err)
	}

	envelope := communityEnvelope(t, &proof, "post-1")
	input := publishInput(envelope)

	input.Checkpoint.Entries[0].SubjectKey = "UC_OTHER"
	input.Checkpoint.Entries[0].ScopeSHA256 = strings.Repeat("b", 64)
	input.Checkpoint.Entries[0].LastObservationKey = "new-unrelated-observation"
	input.Checkpoint.Entries[0].LastEvidenceSHA256 = strings.Repeat("d", 64)

	_, err := NewRepository(pool).PublishBatch(ctx, input)

	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("unrelated checkpoint error = %v, want ErrInvalidEnvelope", err)
	}

	assertTableCount(t, pool, "source_observations", 0)
	assertTableCount(t, pool, "source_observation_queue", 0)

	var observationKey, evidence string

	if err := pool.QueryRow(ctx, `
		SELECT last_observation_key, last_evidence_sha256
		FROM source_collection_checkpoints
		WHERE provider = 'youtubejs' AND observation_kind = 'community_page'
		  AND subject_key = 'UC_OTHER' AND scope_sha256 = $1
	`, strings.Repeat("b", 64)).Scan(&observationKey, &evidence); err != nil {
		t.Fatalf("load unrelated checkpoint: %v", err)
	}

	if observationKey != "old-observation" || evidence != oldEvidence {
		t.Fatalf("unrelated checkpoint mutated: key=%s evidence=%s", observationKey, evidence)
	}
}

func TestPublishBatchRejectsMissingCheckpointWithoutWrites(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	input := publishInput(communityEnvelope(t, &proof, "post-1"))

	input.Checkpoint.Entries = nil

	_, err := NewRepository(pool).PublishBatch(ctx, input)

	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("missing checkpoint error = %v, want ErrInvalidEnvelope", err)
	}

	assertTableCount(t, pool, "source_observations", 0)
	assertTableCount(t, pool, "source_observation_queue", 0)
	assertTableCount(t, pool, "source_collection_checkpoints", 0)
}

func TestPublishBatchAllowsOneCheckpointPerMultiKindObservation(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindChannelProfile, testChannelID, "youtubejs_channel_metadata")

	insertPublishTarget(ctx, t, pool, proof.ProjectionGeneration, testChannelID, contract.KindChannelPhoto, true)

	profile := channelProfileEnvelope(t, &proof, 1)
	photo := channelPhotoEnvelopeFor(t, &proof, testChannelID)
	input := &PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           []CheckpointEntry{checkpointForEnvelope(profile), checkpointForEnvelope(photo)},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{*profile, *photo},
	}

	result, err := NewRepository(pool).PublishBatch(ctx, input)
	if err != nil {
		t.Fatalf("publish multi-kind batch: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("multi-kind result count = %d, want 2", len(result.Results))
	}

	assertTableCount(t, pool, "source_observations", 2)
	assertTableCount(t, pool, "source_observation_queue", 2)
	assertTableCount(t, pool, "source_collection_checkpoints", 2)
}

func TestPublishBatchRejectsDuplicateCheckpointBinding(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindChannelProfile, testChannelID, "youtubejs_channel_metadata")

	insertPublishTarget(ctx, t, pool, proof.ProjectionGeneration, testChannelID, contract.KindChannelPhoto, true)

	profile := channelProfileEnvelope(t, &proof, 1)
	photo := channelPhotoEnvelopeFor(t, &proof, testChannelID)
	profileCheckpoint := checkpointForEnvelope(profile)
	_, err := NewRepository(pool).PublishBatch(ctx, &PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           []CheckpointEntry{profileCheckpoint, profileCheckpoint},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{*profile, *photo},
	})

	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("duplicate checkpoint error = %v, want ErrInvalidEnvelope", err)
	}

	assertTableCount(t, pool, "source_observations", 0)
	assertTableCount(t, pool, "source_observation_queue", 0)
	assertTableCount(t, pool, "source_collection_checkpoints", 0)
}

func TestPublishBatchRejectsStaleContract(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	envelope := communityEnvelope(t, &proof, "post-1")

	if _, err := pool.Exec(ctx, `
		UPDATE observation_contract_generations
		SET current_generation = 2
		WHERE provider = 'youtubejs' AND observation_kind = 'community_page'
	`); err != nil {
		t.Fatalf("bump contract: %v", err)
	}

	_, err := NewRepository(pool).PublishBatch(ctx, publishInput(envelope))
	if !errors.Is(err, ErrStaleContract) {
		t.Fatalf("publish stale contract error = %v", err)
	}

	assertTableCount(t, pool, "source_observations", 0)
}

func TestPublishBatchRollsBackEvidenceAndCheckpointWhenQueueInsertFails(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")

	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION source_observation_fail_queue_insert() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced queue insert failure';
		END;
		$$ LANGUAGE plpgsql
	`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		CREATE TRIGGER source_observation_fail_queue_insert
		BEFORE INSERT ON source_observation_queue
		FOR EACH ROW EXECUTE FUNCTION source_observation_fail_queue_insert()
	`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := NewRepository(pool).PublishBatch(ctx, publishInput(
		communityEnvelope(t, &proof, "post-1"),
	))
	if err == nil {
		t.Fatal("publish must fail")
	}

	assertTableCount(t, pool, "source_observations", 0)
	assertTableCount(t, pool, "source_observation_queue", 0)
	assertTableCount(t, pool, "source_collection_checkpoints", 0)
}

func TestPublishSetCollisionWriteKeepsInsertOnlyPrivilege(t *testing.T) {
	query := mustSQL("repository_publish_set_0032_32.sql")
	start := strings.Index(query, "collision_write AS (")
	end := strings.Index(query, "), observation_write AS (")

	if start < 0 || end <= start {
		t.Fatal("publish set collision write CTE is missing")
	}

	collisionWrite := query[start:end]
	if !strings.Contains(collisionWrite, "RETURNING 1 AS inserted") || strings.Contains(collisionWrite, "RETURNING id") {
		t.Fatal("collision write must not require SELECT privilege for RETURNING")
	}

	if !strings.Contains(query, "count(inserted) FROM collision_write") {
		t.Fatal("collision write execution barrier must count the constant result")
	}
}

func TestPublishBatchTargetDisableDuringFetchRollsBackEverything(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	envelope := communityEnvelope(t, &proof, "post-1")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, execErr3 := tx.Exec(ctx, `UPDATE youtube_collection_projection_generations SET status = 'RETIRED' WHERE generation = $1`, proof.ProjectionGeneration); execErr3 != nil {
		t.Fatal(execErr3)
	}

	if _, execErr4 := tx.Exec(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until, activated_at
		) VALUES ('CURRENT', 0, repeat('b', 64), clock_timestamp() + INTERVAL '1 hour', clock_timestamp())
	`); execErr4 != nil {
		t.Fatal(execErr4)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatal(commitErr)
	}

	// 새 CURRENT가 이 job의 target을 이어받지 않았으므로 job별 membership 변경(superseded)으로 거절된다.
	_, err = NewRepository(pool).PublishBatch(ctx, publishInput(envelope))
	if !errors.Is(err, collection.ErrTargetDisabled) {
		t.Fatalf("own target removed mid-fetch error = %v", err)
	}

	assertPublishSideEffects(t, pool, 0, 0, 0)

	var state string

	if err := pool.QueryRow(ctx, `SELECT slot_state FROM youtube_collection_job_leases WHERE job_key = $1`, proof.JobKey).Scan(&state); err != nil {
		t.Fatal(err)
	}

	if state != testSlotStateActive {
		t.Fatalf("disabled publish changed job state to %s", state)
	}
}

func TestPublishBatchRejectsOutOfBundleTargetAtomically(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindChannelProfile, testChannelID, "youtubejs_channel_metadata")

	insertPublishTarget(ctx, t, pool, proof.ProjectionGeneration, "UC_OTHER", contract.KindChannelPhoto, true)

	profile := channelProfileEnvelope(t, &proof, 1)
	photo := channelPhotoEnvelopeFor(t, &proof, "UC_OTHER")
	_, err := NewRepository(pool).PublishBatch(ctx, &PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           []CheckpointEntry{checkpointForEnvelope(profile), checkpointForEnvelope(photo)},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{*profile, *photo},
	})

	if !errors.Is(err, collection.ErrTargetDisabled) {
		t.Fatalf("out-of-bundle error = %v", err)
	}

	assertPublishSideEffects(t, pool, 0, 0, 0)
}

func TestPublishBatchGlobalBundleVerifiesEveryTarget(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderHolodex, contract.KindViewerSample, "video-1", "holodex_live")

	insertPublishTarget(ctx, t, pool, proof.ProjectionGeneration, "video-2", contract.KindViewerSample, false)

	first := viewerEnvelopeFor(t, &proof, 1, "video-1", 100)
	second := viewerEnvelopeFor(t, &proof, 1, "video-2", 200)
	_, err := historicalViewerPublisher(t, pool).PublishBatch(ctx, &PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           []CheckpointEntry{checkpointForEnvelope(first), checkpointForEnvelope(second)},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{*first, *second},
	})

	if !errors.Is(err, collection.ErrTargetDisabled) {
		t.Fatalf("global disabled target error = %v", err)
	}

	assertPublishSideEffects(t, pool, 0, 0, 0)
}

func TestStaleHolderCannotMutatePublishOrJobState(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proofA := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	envelopeA := communityEnvelope(t, &proofA, "post-a")
	resumeA := make(chan struct{})
	resultA := make(chan error, 1)

	go func() {
		<-resumeA

		_, err := NewRepository(pool).PublishBatch(ctx, publishInput(envelopeA))
		resultA <- err
	}()

	proofB := proofA

	proofB.OwnerInstance = "collector-b"
	proofB.FenceEpoch++

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET lease_expires_at = clock_timestamp() - INTERVAL '1 second'
		WHERE job_key = $1
	`, proofA.JobKey); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET owner_instance = $2, fence_epoch = $3,
		    lease_expires_at = clock_timestamp() + INTERVAL '1 hour'
		WHERE job_key = $1 AND lease_expires_at <= clock_timestamp()
	`, proofB.JobKey, proofB.OwnerInstance, proofB.FenceEpoch); err != nil {
		t.Fatal(err)
	}

	envelopeB := communityEnvelope(t, &proofB, "post-b")
	if _, err := NewRepository(pool).PublishBatch(ctx, publishInput(envelopeB)); err != nil {
		t.Fatalf("new holder publish: %v", err)
	}

	var nextDueBefore time.Time

	if err := pool.QueryRow(ctx, `SELECT next_due_at FROM youtube_collection_job_leases WHERE job_key = $1`, proofB.JobKey).Scan(&nextDueBefore); err != nil {
		t.Fatal(err)
	}

	close(resumeA)

	if err := <-resultA; !errors.Is(err, collection.ErrFenceLost) {
		t.Fatalf("stale holder error = %v", err)
	}

	assertPublishSideEffects(t, pool, 1, 1, 1)

	var nextDueAfter time.Time

	if err := pool.QueryRow(ctx, `SELECT next_due_at FROM youtube_collection_job_leases WHERE job_key = $1`, proofB.JobKey).Scan(&nextDueAfter); err != nil {
		t.Fatal(err)
	}

	if !nextDueAfter.Equal(nextDueBefore) {
		t.Fatalf("stale holder changed next_due_at: before=%s after=%s", nextDueBefore, nextDueAfter)
	}
}

type collectionJobLeaseState struct {
	slotState string
	owner     string
	epoch     int64
	nextDueAt time.Time
}

func takeOverCollectionJobLease(ctx context.Context, t *testing.T, pool *pgxpool.Pool, proof contract.LeaseProof) contract.LeaseProof {
	t.Helper()

	proof.OwnerInstance = "collector-b"
	proof.FenceEpoch++

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, fence_epoch = $3,
		    lease_expires_at = clock_timestamp() + INTERVAL '1 hour'
		WHERE job_key = $1
	`, proof.JobKey, proof.OwnerInstance, proof.FenceEpoch); err != nil {
		t.Fatal(err)
	}

	return proof
}

func readCollectionJobLeaseState(ctx context.Context, t *testing.T, pool *pgxpool.Pool, jobKey string) collectionJobLeaseState {
	t.Helper()

	var got collectionJobLeaseState

	if err := pool.QueryRow(ctx, `
		SELECT slot_state, owner_instance, fence_epoch, next_due_at
		FROM youtube_collection_job_leases
		WHERE job_key = $1
	`, jobKey).Scan(&got.slotState, &got.owner, &got.epoch, &got.nextDueAt); err != nil {
		t.Fatal(err)
	}

	return got
}

func runStaleHolderCase(t *testing.T, name, postID string) {
	t.Helper()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proofA := seedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	repo := NewRepository(pool)

	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proofA, "post-a"))); err != nil {
		t.Fatalf("publish base: %v", err)
	}

	proofB := takeOverCollectionJobLease(ctx, t, pool, proofA)
	before := readCollectionJobLeaseState(ctx, t, pool, proofB.JobKey)

	candidate := communityEnvelope(t, &proofA, postID)
	if _, err := repo.PublishBatch(ctx, publishInput(candidate)); !errors.Is(err, collection.ErrFenceLost) {
		t.Fatalf("stale %s error = %v", name, err)
	}

	assertPublishSideEffects(t, pool, 1, 1, 1)

	after := readCollectionJobLeaseState(ctx, t, pool, proofB.JobKey)
	if after.slotState != testSlotStateActive || after.owner != proofB.OwnerInstance ||
		after.epoch != proofB.FenceEpoch || !after.nextDueAt.Equal(before.nextDueAt) {
		t.Fatalf("stale %s changed job state: %+v", name, after)
	}
}

func TestStaleHolderCannotCompleteDuplicateOrCollision(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		postID string
	}{
		{name: "duplicate", postID: "post-a"},
		{name: "collision", postID: "post-b"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runStaleHolderCase(t, testCase.name, testCase.postID)
		})
	}
}

// targetQueryCounter는 문장 수와 왕복 수를 따로 센다. 파이프라인 문장은 QueryTracer에 보이지 않으므로
// BatchTracer로 배치 1회를 왕복 1회, 배치 안 문장을 각각 문장으로 센다.
type targetQueryCounter struct {
	queries    atomic.Int32
	roundTrips atomic.Int32
}

func (c *targetQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.queries.Add(1)
	c.roundTrips.Add(1)

	return ctx
}

func (*targetQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *targetQueryCounter) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	c.roundTrips.Add(1)

	return ctx
}

func (c *targetQueryCounter) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {
	c.queries.Add(1)
}

func (*targetQueryCounter) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}

func (c *targetQueryCounter) reset() {
	c.queries.Store(0)
	c.roundTrips.Store(0)
}

func TestPublishBatchRejectsOversizedEncodedSetBeforeDatabaseAccess(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	counter := &targetQueryCounter{}
	config := pool.Config()

	config.ConnConfig.Tracer = counter

	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer tracedPool.Close()

	proof := contract.LeaseProof{
		JobKey: "job:oversized", CollectionJobKind: "community_collect",
		OwnerInstance: "collector-a", FenceEpoch: 1, ProjectionGeneration: 1,
		ScheduledFor: time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC),
	}
	size := MaxPublishBatchBytes/100_000 + 2
	observations := make([]contract.Envelope, size)
	checkpoints := make([]CheckpointEntry, size)

	for i := range observations {
		observations[i] = *oversizedCommunityEnvelope(t, &proof, i)
		checkpoints[i] = checkpointForEnvelope(&observations[i])
	}

	input := &PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries: checkpoints, CollectionLatency: time.Second,
		},
		Observations: observations,
	}

	counter.reset()

	_, err = NewRepository(tracedPool).PublishBatch(ctx, input)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("error = %v, want invalid envelope", err)
	}

	if got, trips := counter.queries.Load(), counter.roundTrips.Load(); got != 0 || trips != 0 {
		t.Fatalf("database statements = %d round trips = %d, want 0", got, trips)
	}
}

func oversizedCommunityEnvelope(t *testing.T, proof *contract.LeaseProof, ordinal int) *contract.Envelope {
	t.Helper()

	subject := fmt.Sprintf("UC_OVERSIZED_%03d", ordinal)

	payload, err := contract.MarshalPayloadV1(contract.CommunityPayloadV1{
		ChannelID: subject,
		Posts: []contract.CommunityPostV1{{
			PostID: fmt.Sprintf("post-%03d", ordinal), ChannelID: subject,
			ContentText: strings.Repeat("x", 100_000),
		}},
		Coverage: contract.CommunityPageCoverageV1{
			ChannelID: subject, MaxResults: 10, PageCount: 1, Exhausted: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindCommunityPage,
		SubjectKey: subject, SchemaVersion: 1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatal(err)
	}

	return &envelope
}

func assertPublishSideEffects(t *testing.T, pool *pgxpool.Pool, observations, queue, checkpoints int) {
	t.Helper()
	assertTableCount(t, pool, "source_observations", observations)
	assertTableCount(t, pool, "source_observation_queue", queue)
	assertTableCount(t, pool, "source_collection_checkpoints", checkpoints)
	assertTableCount(t, pool, "source_observation_collisions", 0)
}

func seedPublishLease(
	ctx context.Context,
	tb testing.TB,
	pool *pgxpool.Pool,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
	jobKind string,
) contract.LeaseProof {
	tb.Helper()

	scheduledFor := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)

	var generation int64

	if err := pool.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until, activated_at
		) VALUES ('CURRENT', 1, $1, NOW() + INTERVAL '1 day', NOW())
		RETURNING generation
	`, strings.Repeat("a", 64)).Scan(&generation); err != nil {
		tb.Fatalf("seed projection: %v", err)
	}

	insertPublishTarget(ctx, tb, pool, generation, subjectKey, kind, true)

	proof := contract.LeaseProof{
		JobKey:               "job:" + jobKind + ":" + subjectKey,
		CollectionJobKind:    jobKind,
		OwnerInstance:        "collector-a",
		FenceEpoch:           1,
		ProjectionGeneration: generation,
		ScheduledFor:         scheduledFor,
	}
	jobClass := "SUBJECT"

	if strings.HasPrefix(jobKind, "holodex_") || jobKind == "official_schedule" {
		jobClass = "GLOBAL"
	}

	scope := publishFixtureScope(tb, provider, jobKind)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at,
			membership_kinds, membership_exact_subject
		) VALUES ($1, $2, $3, $4, $5, $6, 60000, 'ACTIVE', $7, $7, $8, $9, NOW() + INTERVAL '1 hour', $10::text[], $11)
	`, proof.JobKey, provider, jobClass, jobKind, subjectKey, generation,
		proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance, scope.Kinds, scope.ExactSubject); err != nil {
		tb.Fatalf("seed lease: %v", err)
	}

	resyncPublishMembership(ctx, tb, pool)

	return proof
}

// publishFixtureScope는 시드 lease에 acquire와 같은 membership 범위를 기록한다. 과거 viewer 계약까지 포함한 집합에서
// 찾으며, 계약이 없는 job은 빈 범위로 남겨 fail closed한다.
func publishFixtureScope(tb testing.TB, provider contract.Provider, jobKind string) collection.MembershipScope {
	tb.Helper()

	definition, ok := historicalViewerJobContracts(tb).Definition(collection.JobID{Provider: provider, Kind: collection.JobKind(jobKind)})
	if !ok {
		return collection.MembershipScope{Kinds: []string{}}
	}

	return collection.MembershipScopeFor(definition)
}

// insertPublishTarget은 이 generation부터 이어지는 활성/비활성 대상을 넣고, 획득 시점 roster로 보고 ACTIVE lease의
// membership 행 수를 다시 맞춘다.
func insertPublishTarget(
	ctx context.Context,
	tb testing.TB,
	pool *pgxpool.Pool,
	generation int64,
	subjectKey string,
	kind contract.ObservationKind,
	enabled bool,
) {
	tb.Helper()

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, member_since_generation
		) VALUES ($1, $2, $3, 50, 60000, $4, $1)
		ON CONFLICT (projection_generation, subject_key, observation_kind) DO NOTHING
	`, generation, subjectKey, kind, enabled); err != nil {
		tb.Fatalf("seed target: %v", err)
	}

	resyncPublishMembership(ctx, tb, pool)
}

func resyncPublishMembership(ctx context.Context, tb testing.TB, pool *pgxpool.Pool) {
	tb.Helper()

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases AS job
		SET membership_target_count = (
			SELECT count(*)
			FROM youtube_collection_targets AS target
			WHERE target.projection_generation = job.projection_generation
			  AND target.observation_kind = ANY(job.membership_kinds)
			  AND target.enabled
			  AND (NOT job.membership_exact_subject OR target.subject_key = job.subject_key)
		)
		WHERE job.slot_state = 'ACTIVE'
	`); err != nil {
		tb.Fatalf("resync lease membership: %v", err)
	}
}

func reactivateLease(t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
		    retry_not_before = NULL, last_error_code = NULL
		WHERE job_key = $1
	`, proof.JobKey, proof.OwnerInstance); err != nil {
		t.Fatalf("reactivate lease: %v", err)
	}
}

// advanceLease는 fence epoch를 올리고 lease를 1분 뒤 다음 slot으로 옮겨 다시 ACTIVE로 만든다.
func advanceLease(
	ctx context.Context,
	tb testing.TB,
	pool *pgxpool.Pool,
	proof *contract.LeaseProof,
) contract.LeaseProof {
	tb.Helper()

	next := *proof
	next.FenceEpoch++

	next.ScheduledFor = next.ScheduledFor.Add(time.Minute)

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
		    retry_not_before = NULL, fence_epoch = $3, scheduled_for = $4, next_due_at = $4
		WHERE job_key = $1
	`, next.JobKey, next.OwnerInstance, next.FenceEpoch, next.ScheduledFor); err != nil {
		tb.Fatalf("advance lease: %v", err)
	}

	return next
}

func communityEnvelope(
	tb testing.TB,
	proof *contract.LeaseProof,
	postID string,
) *contract.Envelope {
	tb.Helper()

	payload, err := contract.MarshalPayloadV1(contract.CommunityPayloadV1{
		ChannelID: testChannelID,
		Posts:     []contract.CommunityPostV1{{PostID: postID, ChannelID: testChannelID}},
		Coverage: contract.CommunityPageCoverageV1{
			ChannelID: testChannelID, MaxResults: 10, PageCount: 1, Exhausted: true,
		},
	})
	if err != nil {
		tb.Fatalf("marshal community payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindCommunityPage,
		SubjectKey:         testChannelID,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: 1,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		tb.Fatalf("prepare community envelope: %v", err)
	}

	return &envelope
}

func viewerEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	count int64,
) *contract.Envelope {
	t.Helper()

	return viewerEnvelopeFor(t, proof, generation, "video-1", count)
}

func viewerEnvelopeFor(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	subject string,
	count int64,
) *contract.Envelope {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ViewerSampleV1{
		VideoID:             subject,
		ViewerCount:         &count,
		Availability:        "AVAILABLE",
		SampleWindowStart:   proof.ScheduledFor,
		SampleWindowSeconds: 60,
		Coverage: contract.ViewerSampleCoverageV1{
			VideoID: subject, SampleWindowStart: proof.ScheduledFor, SampleWindowSeconds: 60,
		},
	})
	if err != nil {
		t.Fatalf("marshal viewer payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderHolodex,
		ObservationKind:    contract.KindViewerSample,
		SubjectKey:         subject,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: generation,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityNotApplicable,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		t.Fatalf("prepare viewer envelope: %v", err)
	}

	return &envelope
}

func channelPhotoEnvelopeFor(
	t *testing.T,
	proof *contract.LeaseProof,
	subject string,
) *contract.Envelope {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ChannelPhotoV1{
		ChannelID: subject,
		Variants:  []contract.PhotoVariantV1{{Kind: "avatar", URL: "https://img.test/avatar.jpg"}},
		Coverage:  contract.ChannelPhotoCoverageV1{ChannelID: subject, Variants: []string{"avatar"}},
	})
	if err != nil {
		t.Fatalf("marshal channel photo payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindChannelPhoto,
		SubjectKey: subject, SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare channel photo envelope: %v", err)
	}

	return &envelope
}

func channelProfileEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
) *contract.Envelope {
	t.Helper()

	return channelProfileEnvelopeFor(t, proof, generation, testChannelID)
}

func channelProfileEnvelopeFor(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	subject string,
) *contract.Envelope {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.ChannelProfileV1{
		ChannelID: subject,
		Handle:    contract.FieldValue[string]{Present: true, Value: "test"},
		Coverage: contract.ChannelProfileCoverageV1{
			ChannelID: subject, Fields: []string{"handle"},
		},
	})
	if err != nil {
		t.Fatalf("marshal channel profile payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindChannelProfile,
		SubjectKey:         subject,
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: generation,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              *proof,
	})
	if err != nil {
		t.Fatalf("prepare channel profile envelope: %v", err)
	}

	return &envelope
}

func checkpointForEnvelope(envelope *contract.Envelope) CheckpointEntry {
	return CheckpointEntry{
		Provider:           envelope.Provider,
		ObservationKind:    envelope.ObservationKind,
		SubjectKey:         envelope.SubjectKey,
		ScopeSHA256:        envelope.ScopeSHA256,
		ContractGeneration: envelope.ContractGeneration,
		LastObservationKey: envelope.ObservationKey,
		LastEvidenceSHA256: envelope.EvidenceSHA256,
		LastScheduledFor:   envelope.ScheduledFor,
		Continuity:         envelope.Continuity,
	}
}

func publishInput(envelope *contract.Envelope) *PublishBatchInput {
	return &PublishBatchInput{
		Lease: envelope.Lease,
		Checkpoint: CheckpointUpdate{
			Entries: []CheckpointEntry{func() CheckpointEntry {
				entry := checkpointForEnvelope(envelope)

				entry.Cursor = jsontext.Value(`{"page":1}`)

				return entry
			}()},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{*envelope},
	}
}

func assertTableCount(t *testing.T, pool *pgxpool.Pool, table string, want int) {
	t.Helper()

	var count int

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}

	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}
