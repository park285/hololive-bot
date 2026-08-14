package sourceobservation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestPublishBatchDuplicateKeepsOneEvidenceAndQueueRow(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	envelope := communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")
	repo := NewRepository(pool)
	first, err := repo.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if first.Results[0].Outcome != PublishInserted {
		t.Fatalf("first outcome = %s", first.Results[0].Outcome)
	}
	reactivateLease(t, pool, proof)
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	base := communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")
	repo := NewRepository(pool)
	if _, err := repo.PublishBatch(ctx, publishInput(base)); err != nil {
		t.Fatalf("publish base: %v", err)
	}

	for _, mutate := range []func(contract.Envelope) contract.Envelope{
		func(envelope contract.Envelope) contract.Envelope {
			return communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-2")
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
		reactivateLease(t, pool, proof)
		collision := mutate(base)
		result, err := repo.PublishBatch(ctx, publishInput(collision))
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	firstProof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	first := communityEnvelope(t, firstProof, 1, contract.CompletenessComplete, "post-1")
	if _, err := repo.PublishBatch(ctx, publishInput(first)); err != nil {
		t.Fatalf("publish first slot: %v", err)
	}
	secondProof := advanceLease(t, pool, firstProof, time.Minute)
	second := communityEnvelope(t, secondProof, 1, contract.CompletenessComplete, "post-1")
	if first.PayloadSHA256 != second.PayloadSHA256 || first.ObservationKey == second.ObservationKey {
		t.Fatalf("payload/identity mismatch across slots: first=%s second=%s", first.ObservationKey, second.ObservationKey)
	}
	if _, err := repo.PublishBatch(ctx, publishInput(second)); err != nil {
		t.Fatalf("publish second slot: %v", err)
	}
	assertTableCount(t, pool, "source_observations", 2)
	assertTableCount(t, pool, "source_observation_queue", 2)
}

func TestPublishBatchViewerEqualValueNextWindowCreatesTwoObservations(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	firstProof := seedPublishLease(t, pool, contract.ProviderHolodex, contract.KindViewerSample, "video-1", "holodex_global")
	repo := NewRepository(pool)
	first := viewerEnvelope(t, firstProof, 1, 100)
	if _, err := repo.PublishBatch(ctx, publishInput(first)); err != nil {
		t.Fatalf("publish first sample: %v", err)
	}
	secondProof := advanceLease(t, pool, firstProof, time.Minute)
	second := viewerEnvelope(t, secondProof, 1, 100)
	if _, err := repo.PublishBatch(ctx, publishInput(second)); err != nil {
		t.Fatalf("publish second sample: %v", err)
	}
	assertTableCount(t, pool, "source_observations", 2)
}

func TestRetryRequiresUnexpiredClaim(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}
	observation := batch.Observations[0]
	expireObservationClaim(t, pool, observation.ID)
	_, err = repo.Retry(ctx, RetryInput{
		ObservationID: observation.ID,
		LeaseToken:    observation.LeaseToken,
		Delay:         time.Second,
		ErrorCode:     "provider_error",
		ErrorDetail:   "temporary provider failure",
	})
	if !errors.Is(err, ErrClaimLost) {
		t.Fatalf("expired retry error = %v, want ErrClaimLost", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM source_observation_queue WHERE observation_id = $1
	`, observation.ID).Scan(&status); err != nil {
		t.Fatalf("load queue after expired retry: %v", err)
	}
	if status != string(contract.StatusProcessing) {
		t.Fatalf("expired retry changed status to %s", status)
	}
}

func TestDeadLetterRequiresUnexpiredClaim(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}
	observation := batch.Observations[0]
	expireObservationClaim(t, pool, observation.ID)
	err = repo.DeadLetter(ctx, DeadLetterInput{
		ObservationID: observation.ID,
		LeaseToken:    observation.LeaseToken,
		ErrorCode:     "unsupported_contract",
		ErrorDetail:   "unsupported test contract",
	})
	if !errors.Is(err, ErrClaimLost) {
		t.Fatalf("expired dead letter error = %v, want ErrClaimLost", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM source_observation_queue WHERE observation_id = $1
	`, observation.ID).Scan(&status); err != nil {
		t.Fatalf("load queue after expired dead letter: %v", err)
	}
	if status != string(contract.StatusProcessing) {
		t.Fatalf("expired dead letter changed status to %s", status)
	}
}

func TestRetryExpiryAfterQueueRowLockWaitReturnsClaimLost(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}
	observation := batch.Observations[0]
	locker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin queue locker: %v", err)
	}
	defer func() { _ = locker.Rollback(ctx) }()
	var lockedID int64
	if err := locker.QueryRow(ctx, `
		SELECT observation_id
		FROM source_observation_queue
		WHERE observation_id = $1
		FOR UPDATE
	`, observation.ID).Scan(&lockedID); err != nil {
		t.Fatalf("lock queue row: %v", err)
	}
	if lockedID != observation.ID {
		t.Fatalf("locked observation id = %d, want %d", lockedID, observation.ID)
	}
	if _, err := locker.Exec(ctx, `
		UPDATE source_observation_queue
		SET lease_expires_at = clock_timestamp() - INTERVAL '1 second'
		WHERE observation_id = $1
	`, observation.ID); err != nil {
		t.Fatalf("expire locked claim: %v", err)
	}
	retryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	retryResult := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, retryErr := repo.Retry(retryCtx, RetryInput{
			ObservationID: observation.ID,
			LeaseToken:    observation.LeaseToken,
			Delay:         time.Second,
			ErrorCode:     "provider_error",
			ErrorDetail:   "row-lock expiry regression",
		})
		retryResult <- retryErr
	}()
	<-started
	select {
	case retryErr := <-retryResult:
		t.Fatalf("retry completed while queue row was locked: %v", retryErr)
	case <-time.After(100 * time.Millisecond):
	}
	if err := locker.Commit(ctx); err != nil {
		t.Fatalf("commit expired queue row: %v", err)
	}
	if err := <-retryResult; !errors.Is(err, ErrClaimLost) {
		t.Fatalf("retry after queue-row lock expiry error = %v, want ErrClaimLost", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM source_observation_queue WHERE observation_id = $1
	`, observation.ID).Scan(&status); err != nil {
		t.Fatalf("load queue after retry: %v", err)
	}
	if status != string(contract.StatusProcessing) {
		t.Fatalf("retry after expiry changed status to %s", status)
	}
}

func TestPublishBatchRejectsUnrelatedCheckpointWithoutWrites(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, 'UC_OTHER', 'community_page', 50, 60000, TRUE, NOW() + INTERVAL '1 day')
	`, proof.ProjectionGeneration); err != nil {
		t.Fatalf("seed unrelated target: %v", err)
	}
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
	envelope := communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	input := publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"))
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindChannelStats, "UC_TEST", "youtubejs_channel")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, 'UC_TEST', 'channel_profile', 50, 60000, TRUE, NOW() + INTERVAL '1 day')
	`, proof.ProjectionGeneration); err != nil {
		t.Fatalf("seed second kind target: %v", err)
	}
	stats := channelStatsEnvelope(t, proof, 1)
	profile := channelProfileEnvelope(t, proof, 1)
	input := PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           []CheckpointEntry{checkpointForEnvelope(stats), checkpointForEnvelope(profile)},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{stats, profile},
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
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindChannelStats, "UC_TEST", "youtubejs_channel")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, 'UC_TEST', 'channel_profile', 50, 60000, TRUE, NOW() + INTERVAL '1 day')
	`, proof.ProjectionGeneration); err != nil {
		t.Fatalf("seed second kind target: %v", err)
	}
	stats := channelStatsEnvelope(t, proof, 1)
	profile := channelProfileEnvelope(t, proof, 1)
	statsCheckpoint := checkpointForEnvelope(stats)
	_, err := NewRepository(pool).PublishBatch(ctx, PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           []CheckpointEntry{statsCheckpoint, statsCheckpoint},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{stats, profile},
	})
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("duplicate checkpoint error = %v, want ErrInvalidEnvelope", err)
	}
	assertTableCount(t, pool, "source_observations", 0)
	assertTableCount(t, pool, "source_observation_queue", 0)
	assertTableCount(t, pool, "source_collection_checkpoints", 0)
}

func TestPublishBatchRejectsStaleContract(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	envelope := communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")
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

func TestClaimDoesNotStrandSupportedOldGenerationAfterCurrentBump(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	envelope := communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")
	repo := NewRepository(pool)
	if _, err := repo.PublishBatch(ctx, publishInput(envelope)); err != nil {
		t.Fatalf("publish generation one: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE observation_contract_generations
		SET current_generation = 2
		WHERE provider = 'youtubejs' AND observation_kind = 'community_page'
	`); err != nil {
		t.Fatalf("bump contract: %v", err)
	}
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil {
		t.Fatalf("claim old supported generation: %v", err)
	}
	if len(batch.Observations) != 1 || batch.Observations[0].ContractGeneration != 1 {
		t.Fatalf("claimed observations = %#v", batch.Observations)
	}
}

func TestFinalizeUnsupportedContractDeadLettersWithBoundedAudit(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	envelope := communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")
	if _, err := NewRepository(pool).PublishBatch(ctx, publishInput(envelope)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	repo := NewRepositoryWithContracts(pool, StaticSupportedContracts{}, InitialJobContracts(), nil)
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}
	observation := batch.Observations[0]
	result, err := repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ID,
		LeaseToken:    observation.LeaseToken,
	}, func(context.Context, dbx.Tx, Observation) (ReconcileResult, error) {
		t.Fatal("unsupported contract must not invoke reconcile")
		return ReconcileResult{}, nil
	})
	if err != nil || !result.Unsupported {
		t.Fatalf("finalize unsupported: result=%#v err=%v", result, err)
	}
	var status, code, detail string
	if err := pool.QueryRow(ctx, `
		SELECT status, last_error_code, last_error_detail
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observation.ID).Scan(&status, &code, &detail); err != nil {
		t.Fatalf("load DLQ: %v", err)
	}
	if status != "DEAD_LETTER" || code != "unsupported_contract" || len(detail) > maxErrorTextBytes {
		t.Fatalf("DLQ status=%s code=%s detail_bytes=%d", status, code, len(detail))
	}
}

func TestFinalizeFutureSourceEventFallsBackToScheduledSlot(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	envelope := communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")
	future := time.Now().UTC().Add(time.Hour)
	envelope.SourceEventAt = &future
	var err error
	envelope, err = contract.PrepareEnvelope(envelope)
	if err != nil {
		t.Fatalf("prepare future source event: %v", err)
	}
	repo := NewRepository(pool)
	if _, err := repo.PublishBatch(ctx, publishInput(envelope)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}
	observation := batch.Observations[0]
	result, err := repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ID,
		LeaseToken:    observation.LeaseToken,
	}, func(_ context.Context, _ dbx.Tx, claimed Observation) (ReconcileResult, error) {
		if !claimed.SourceEventFallback || !claimed.EffectiveAt.Equal(claimed.ScheduledFor) {
			t.Fatalf("claimed clock = %#v", claimed)
		}
		return ReconcileResult{}, nil
	})
	if err != nil || !result.SourceEventFallback || !result.EffectiveAt.Equal(envelope.ScheduledFor) {
		t.Fatalf("finalize clock result=%#v err=%v", result, err)
	}
}

func TestFinalizeLeaseExpiryAtTerminalUpdateRollsBackAllSideEffects(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}
	observation := batch.Observations[0]
	var leaseExpiresAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT lease_expires_at
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observation.ID).Scan(&leaseExpiresAt); err != nil {
		t.Fatalf("load initial lease: %v", err)
	}
	if !leaseExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("claim did not begin with a valid lease: %s", leaseExpiresAt)
	}
	_, err = repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ID,
		LeaseToken:    observation.LeaseToken,
	}, func(ctx context.Context, tx dbx.Tx, claimed Observation) (ReconcileResult, error) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO youtube_community_posts (post_id, channel_id)
			VALUES ('finalize-expiry-post', $1)
		`, claimed.SubjectKey); err != nil {
			return ReconcileResult{}, fmt.Errorf("seed canonical side effect: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE source_observation_queue
			SET lease_expires_at = clock_timestamp() - INTERVAL '1 second'
			WHERE observation_id = $1
		`, claimed.ID); err != nil {
			return ReconcileResult{}, fmt.Errorf("expire claim before terminal update: %w", err)
		}
		return ReconcileResult{
			Applications: []Application{{
				EntityKind: "community_post", EntityKey: "finalize-expiry-post", Decision: "UPSERT",
			}},
		}, nil
	})
	if !errors.Is(err, ErrClaimLost) {
		t.Fatalf("expired terminal finalize error = %v, want ErrClaimLost", err)
	}
	assertTableCount(t, pool, "youtube_community_posts", 0)
	assertTableCount(t, pool, "source_observation_applications", 0)
	assertTableCount(t, pool, "source_observation_consumer_offsets", 0)
	var status string
	var leaseToken string
	if err := pool.QueryRow(ctx, `
		SELECT status, lease_token
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observation.ID).Scan(&status, &leaseToken); err != nil {
		t.Fatalf("load queue after rollback: %v", err)
	}
	if status != string(contract.StatusProcessing) || leaseToken != observation.LeaseToken {
		t.Fatalf("queue side effect committed: status=%s lease_token=%s", status, leaseToken)
	}
}

func TestFinalizeUnsupportedContractExpiryAtDeadLetterRollsBackState(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := NewRepository(pool).PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	claimOptions := claimOptions()
	claimOptions.LeaseDuration = 2 * time.Second
	batch, err := NewRepository(pool).ClaimBatch(ctx, claimOptions)
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}
	observation := batch.Observations[0]
	var leaseExpiresAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT lease_expires_at
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observation.ID).Scan(&leaseExpiresAt); err != nil {
		t.Fatalf("load initial lease: %v", err)
	}
	if !leaseExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("DLQ regression did not begin with a valid lease: %s", leaseExpiresAt)
	}
	repo := NewRepositoryWithContracts(
		pool,
		delayedUnsupportedContracts{delay: 2500 * time.Millisecond},
		InitialJobContracts(),
		nil,
	)
	err = nil
	_, err = repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ID,
		LeaseToken:    observation.LeaseToken,
	}, func(context.Context, dbx.Tx, Observation) (ReconcileResult, error) {
		t.Fatal("unsupported contract must not invoke reconcile")
		return ReconcileResult{}, nil
	})
	if !errors.Is(err, ErrClaimLost) {
		t.Fatalf("expired unsupported-contract DLQ error = %v, want ErrClaimLost", err)
	}
	assertTableCount(t, pool, "youtube_community_posts", 0)
	assertTableCount(t, pool, "source_observation_applications", 0)
	assertTableCount(t, pool, "source_observation_consumer_offsets", 0)
	var status, lastErrorCode, leaseToken string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(last_error_code, ''), COALESCE(lease_token, '')
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observation.ID).Scan(&status, &lastErrorCode, &leaseToken); err != nil {
		t.Fatalf("load queue after expired DLQ rollback: %v", err)
	}
	if status != string(contract.StatusProcessing) || lastErrorCode != "" || leaseToken != observation.LeaseToken {
		t.Fatalf("DLQ side effect committed: status=%s error=%s lease_token=%s", status, lastErrorCode, leaseToken)
	}
}

func TestPublishBatchRollsBackEvidenceAndCheckpointWhenQueueInsertFails(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
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
		communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"),
	))
	if err == nil {
		t.Fatal("publish must fail")
	}
	assertTableCount(t, pool, "source_observations", 0)
	assertTableCount(t, pool, "source_observation_queue", 0)
	assertTableCount(t, pool, "source_collection_checkpoints", 0)
}

func TestReplayReactivatesTerminalQueueWithoutCopyingEvidence(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Observations) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}
	observation := batch.Observations[0]
	if _, err := repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ID,
		LeaseToken:    observation.LeaseToken,
	}, func(context.Context, dbx.Tx, Observation) (ReconcileResult, error) {
		return ReconcileResult{}, nil
	}); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: observation.ID,
		RequestedBy:   "test-operator",
		Reason:        "regression verification",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: result=%#v err=%v", replay, err)
	}
	assertTableCount(t, pool, "source_observations", 1)
	assertTableCount(t, pool, "source_observation_replay_requests", 1)
	var status string
	var replayCount int
	if err := pool.QueryRow(ctx, `
		SELECT status, replay_count FROM source_observation_queue WHERE observation_id = $1
	`, observation.ID).Scan(&status, &replayCount); err != nil {
		t.Fatalf("load replayed queue: %v", err)
	}
	if status != "PENDING" || replayCount != 1 {
		t.Fatalf("status=%s replay_count=%d", status, replayCount)
	}
}

func TestClaimSQLUsesBoundedSkipLockedWithoutGenerationFilter(t *testing.T) {
	query := mustSQL("repository_claim_0012_12.sql")
	if !strings.Contains(query, "LIMIT $2") || !strings.Contains(query, "FOR UPDATE OF queue SKIP LOCKED") {
		t.Fatal("claim query must be bounded and use SKIP LOCKED")
	}
	if strings.Contains(query, "current_generation") {
		t.Fatal("claim query must not filter immutable evidence by current generation")
	}
}

func TestClaimOptionsBounds(t *testing.T) {
	options := claimOptions()
	if err := options.validate(); err != nil {
		t.Fatalf("valid options: %v", err)
	}
	options.Limit = MaxClaimBatchSize + 1
	if err := options.validate(); err == nil {
		t.Fatal("oversized claim must fail")
	}
}

func TestNewLeaseTokenReturnsBoundedLowercaseHex(t *testing.T) {
	token, err := newLeaseToken()
	if err != nil {
		t.Fatalf("new lease token: %v", err)
	}
	if !lowercaseHexToken(token) {
		t.Fatalf("invalid lease token %q", token)
	}
}

func TestPublishBatchTargetDisableDuringFetchRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	envelope := communityEnvelope(t, proof, 1, contract.CompletenessComplete, "post-1")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE youtube_collection_projection_generations SET status = 'RETIRED' WHERE generation = $1`, proof.ProjectionGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until, activated_at
		) VALUES ('CURRENT', 0, repeat('b', 64), clock_timestamp() + INTERVAL '1 hour', clock_timestamp())
	`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = NewRepository(pool).PublishBatch(ctx, publishInput(envelope))
	if !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("disabled mid-fetch error = %v", err)
	}
	assertPublishSideEffects(t, pool, 0, 0, 0, 0)
	var state string
	if err := pool.QueryRow(ctx, `SELECT slot_state FROM youtube_collection_job_leases WHERE job_key = $1`, proof.JobKey).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "ACTIVE" {
		t.Fatalf("disabled publish changed job state to %s", state)
	}
}

func TestPublishBatchRejectsOutOfBundleTargetAtomically(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindChannelStats, "UC_TEST", "youtubejs_channel")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, 'UC_OTHER', 'channel_profile', 50, 60000, TRUE, clock_timestamp() + INTERVAL '1 hour')
	`, proof.ProjectionGeneration); err != nil {
		t.Fatal(err)
	}
	stats := channelStatsEnvelope(t, proof, 1)
	profile := channelProfileEnvelopeFor(t, proof, 1, "UC_OTHER")
	_, err := NewRepository(pool).PublishBatch(ctx, PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           []CheckpointEntry{checkpointForEnvelope(stats), checkpointForEnvelope(profile)},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{stats, profile},
	})
	if !errors.Is(err, ErrTargetDisabled) {
		t.Fatalf("out-of-bundle error = %v", err)
	}
	assertPublishSideEffects(t, pool, 0, 0, 0, 0)
}

func TestPublishBatchGlobalBundleVerifiesEveryTarget(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderHolodex, contract.KindViewerSample, "video-1", "holodex_global")
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, 'video-2', 'viewer_sample', 50, 60000, FALSE, clock_timestamp() + INTERVAL '1 hour')
	`, proof.ProjectionGeneration); err != nil {
		t.Fatal(err)
	}
	first := viewerEnvelopeFor(t, proof, 1, "video-1", 100)
	second := viewerEnvelopeFor(t, proof, 1, "video-2", 200)
	_, err := NewRepository(pool).PublishBatch(ctx, PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           []CheckpointEntry{checkpointForEnvelope(first), checkpointForEnvelope(second)},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{first, second},
	})
	if !errors.Is(err, ErrTargetDisabled) {
		t.Fatalf("global disabled target error = %v", err)
	}
	assertPublishSideEffects(t, pool, 0, 0, 0, 0)
}

func TestStaleHolderCannotMutatePublishOrJobState(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proofA := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	envelopeA := communityEnvelope(t, proofA, 1, contract.CompletenessComplete, "post-a")
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
	envelopeB := communityEnvelope(t, proofB, 1, contract.CompletenessComplete, "post-b")
	if _, err := NewRepository(pool).PublishBatch(ctx, publishInput(envelopeB)); err != nil {
		t.Fatalf("new holder publish: %v", err)
	}
	var nextDueBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT next_due_at FROM youtube_collection_job_leases WHERE job_key = $1`, proofB.JobKey).Scan(&nextDueBefore); err != nil {
		t.Fatal(err)
	}
	close(resumeA)
	if err := <-resultA; !errors.Is(err, ErrCollectionFenceLost) {
		t.Fatalf("stale holder error = %v", err)
	}
	assertPublishSideEffects(t, pool, 1, 1, 1, 0)
	var nextDueAfter time.Time
	if err := pool.QueryRow(ctx, `SELECT next_due_at FROM youtube_collection_job_leases WHERE job_key = $1`, proofB.JobKey).Scan(&nextDueAfter); err != nil {
		t.Fatal(err)
	}
	if !nextDueAfter.Equal(nextDueBefore) {
		t.Fatalf("stale holder changed next_due_at: before=%s after=%s", nextDueBefore, nextDueAfter)
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
			ctx := context.Background()
			pool := dbtest.NewPool(t)
			proofA := seedPublishLease(t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
			base := communityEnvelope(t, proofA, 1, contract.CompletenessComplete, "post-a")
			repo := NewRepository(pool)
			if _, err := repo.PublishBatch(ctx, publishInput(base)); err != nil {
				t.Fatalf("publish base: %v", err)
			}

			proofB := proofA
			proofB.OwnerInstance = "collector-b"
			proofB.FenceEpoch++
			if _, err := pool.Exec(ctx, `
				UPDATE youtube_collection_job_leases
				SET slot_state = 'ACTIVE', owner_instance = $2, fence_epoch = $3,
				    lease_expires_at = clock_timestamp() + INTERVAL '1 hour'
				WHERE job_key = $1
			`, proofB.JobKey, proofB.OwnerInstance, proofB.FenceEpoch); err != nil {
				t.Fatal(err)
			}
			var nextDueBefore time.Time
			if err := pool.QueryRow(ctx, `
				SELECT next_due_at
				FROM youtube_collection_job_leases
				WHERE job_key = $1
			`, proofB.JobKey).Scan(&nextDueBefore); err != nil {
				t.Fatal(err)
			}

			candidate := communityEnvelope(t, proofA, 1, contract.CompletenessComplete, testCase.postID)
			if _, err := repo.PublishBatch(ctx, publishInput(candidate)); !errors.Is(err, ErrCollectionFenceLost) {
				t.Fatalf("stale %s error = %v", testCase.name, err)
			}
			assertPublishSideEffects(t, pool, 1, 1, 1, 0)

			var state string
			var owner string
			var epoch int64
			var nextDueAfter time.Time
			if err := pool.QueryRow(ctx, `
				SELECT slot_state, owner_instance, fence_epoch, next_due_at
				FROM youtube_collection_job_leases
				WHERE job_key = $1
			`, proofB.JobKey).Scan(&state, &owner, &epoch, &nextDueAfter); err != nil {
				t.Fatal(err)
			}
			if state != "ACTIVE" || owner != proofB.OwnerInstance || epoch != proofB.FenceEpoch || !nextDueAfter.Equal(nextDueBefore) {
				t.Fatalf(
					"stale %s changed job state: state=%s owner=%s epoch=%d next_due_at=%s",
					testCase.name,
					state,
					owner,
					epoch,
					nextDueAfter,
				)
			}
		})
	}
}

type targetQueryCounter struct {
	queries atomic.Int32
}

func (c *targetQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.queries.Add(1)
	return ctx
}

func (*targetQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestPublishTargetVerificationQueryCountIsConstantAtMaxBatch(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderHolodex, contract.KindViewerSample, "video-000", "holodex_global")
	subjects := make([]string, MaxPublishBatchSize-1)
	kinds := make([]string, MaxPublishBatchSize-1)
	for i := range subjects {
		subjects[i] = fmt.Sprintf("video-%03d", i+1)
		kinds[i] = string(contract.KindViewerSample)
	}
	if _, err := pool.Exec(ctx, mustTestSQL("insert_publish_targets.sql"), proof.ProjectionGeneration, subjects, kinds); err != nil {
		t.Fatal(err)
	}
	observations := make([]contract.Envelope, MaxPublishBatchSize)
	for i := range observations {
		observations[i] = contract.Envelope{
			Provider: contract.ProviderHolodex, ObservationKind: contract.KindViewerSample,
			SubjectKey: fmt.Sprintf("video-%03d", i),
		}
	}
	counter := &targetQueryCounter{}
	config := pool.Config()
	config.ConnConfig.Tracer = counter
	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer tracedPool.Close()
	tx, err := tracedPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	counter.queries.Store(0)
	if err := (sqlPublishFenceVerifier{jobs: InitialJobContracts()}).Verify(ctx, tx, proof, observations); err != nil {
		t.Fatalf("verify max batch: %v", err)
	}
	if got := counter.queries.Load(); got != 3 {
		t.Fatalf("target fence queries = %d, want constant 3", got)
	}
}

func TestPublishBatchStatementCountIsConstant(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, pool, contract.ProviderHolodex, contract.KindViewerSample, "video-000", "holodex_global")
	subjects := make([]string, MaxPublishBatchSize-1)
	kinds := make([]string, MaxPublishBatchSize-1)
	for i := range subjects {
		subjects[i] = fmt.Sprintf("video-%03d", i+1)
		kinds[i] = string(contract.KindViewerSample)
	}
	if _, err := pool.Exec(ctx, mustTestSQL("insert_publish_targets.sql"), proof.ProjectionGeneration, subjects, kinds); err != nil {
		t.Fatal(err)
	}
	counter := &targetQueryCounter{}
	config := pool.Config()
	config.ConnConfig.Tracer = counter
	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer tracedPool.Close()
	repository := NewRepository(tracedPool)
	for _, size := range []int{1, 361, MaxPublishBatchSize} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			input := viewerPublishBatch(t, proof, size)
			encoded, contracts, err := encodePublishBatch(input)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := tracedPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			counter.queries.Store(0)
			if _, err := repository.publishBatchTx(ctx, tx, input, encoded, contracts); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatalf("publish %d observations: %v", size, err)
			}
			if got := counter.queries.Load(); got != 6 {
				_ = tx.Rollback(ctx)
				t.Fatalf("publish statements = %d, want constant 6", got)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublishBatchRejectsOversizedEncodedSetBeforeDatabaseAccess(t *testing.T) {
	ctx := context.Background()
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
		ScheduledFor: time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
	}
	size := MaxPublishBatchBytes/100_000 + 2
	observations := make([]contract.Envelope, size)
	checkpoints := make([]CheckpointEntry, size)
	for i := range observations {
		observations[i] = oversizedCommunityEnvelope(t, proof, i)
		checkpoints[i] = checkpointForEnvelope(observations[i])
	}
	input := PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries: checkpoints, CollectionLatency: time.Second,
		},
		Observations: observations,
	}
	counter.queries.Store(0)
	_, err = NewRepository(tracedPool).PublishBatch(ctx, input)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("error = %v, want invalid envelope", err)
	}
	if got := counter.queries.Load(); got != 0 {
		t.Fatalf("database statements = %d, want 0", got)
	}
}

func viewerPublishBatch(t *testing.T, proof contract.LeaseProof, size int) PublishBatchInput {
	t.Helper()
	observations := make([]contract.Envelope, size)
	checkpoints := make([]CheckpointEntry, size)
	for i := range observations {
		observations[i] = viewerEnvelopeFor(t, proof, 1, fmt.Sprintf("video-%03d", i), int64(i+1))
		checkpoints[i] = checkpointForEnvelope(observations[i])
	}
	return PublishBatchInput{
		Lease: proof,
		Checkpoint: CheckpointUpdate{
			Entries:           checkpoints,
			CollectionLatency: time.Second,
		},
		Observations: observations,
	}
}

func oversizedCommunityEnvelope(t *testing.T, proof contract.LeaseProof, ordinal int) contract.Envelope {
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
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: proof,
	})
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func assertPublishSideEffects(t *testing.T, pool *pgxpool.Pool, observations, queue, checkpoints, collisions int) {
	t.Helper()
	assertTableCount(t, pool, "source_observations", observations)
	assertTableCount(t, pool, "source_observation_queue", queue)
	assertTableCount(t, pool, "source_collection_checkpoints", checkpoints)
	assertTableCount(t, pool, "source_observation_collisions", collisions)
}

func seedPublishLease(
	t *testing.T,
	pool *pgxpool.Pool,
	provider contract.Provider,
	kind contract.ObservationKind,
	subjectKey string,
	jobKind string,
) contract.LeaseProof {
	t.Helper()
	ctx := context.Background()
	scheduledFor := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	var generation int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until, activated_at
		) VALUES ('CURRENT', 1, $1, NOW() + INTERVAL '1 day', NOW())
		RETURNING generation
	`, strings.Repeat("a", 64)).Scan(&generation); err != nil {
		t.Fatalf("seed projection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		) VALUES ($1, $2, $3, 50, 60000, TRUE, NOW() + INTERVAL '1 day')
	`, generation, subjectKey, kind); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	proof := contract.LeaseProof{
		JobKey:               "job:" + jobKind + ":" + subjectKey,
		CollectionJobKind:    jobKind,
		OwnerInstance:        "collector-a",
		FenceEpoch:           1,
		ProjectionGeneration: generation,
		ScheduledFor:         scheduledFor,
	}
	jobClass := "SUBJECT"
	if jobKind == "holodex_global" || jobKind == "official_schedule" {
		jobClass = "GLOBAL"
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, fence_epoch, owner_instance, lease_expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, 60000, 'ACTIVE', $7, $7, $8, $9, NOW() + INTERVAL '1 hour')
	`, proof.JobKey, provider, jobClass, jobKind, subjectKey, generation,
		proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance); err != nil {
		t.Fatalf("seed lease: %v", err)
	}
	return proof
}

func reactivateLease(t *testing.T, pool *pgxpool.Pool, proof contract.LeaseProof) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
		    retry_not_before = NULL, last_error_code = NULL
		WHERE job_key = $1
	`, proof.JobKey, proof.OwnerInstance); err != nil {
		t.Fatalf("reactivate lease: %v", err)
	}
}

func advanceLease(
	t *testing.T,
	pool *pgxpool.Pool,
	proof contract.LeaseProof,
	delta time.Duration,
) contract.LeaseProof {
	t.Helper()
	proof.FenceEpoch++
	proof.ScheduledFor = proof.ScheduledFor.Add(delta)
	if _, err := pool.Exec(context.Background(), `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
		    retry_not_before = NULL, fence_epoch = $3, scheduled_for = $4, next_due_at = $4
		WHERE job_key = $1
	`, proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ScheduledFor); err != nil {
		t.Fatalf("advance lease: %v", err)
	}
	return proof
}

func expireObservationClaim(t *testing.T, pool *pgxpool.Pool, observationID int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE source_observation_queue
		SET lease_expires_at = NOW() - INTERVAL '1 second'
		WHERE observation_id = $1
	`, observationID); err != nil {
		t.Fatalf("expire observation claim: %v", err)
	}
}

type delayedUnsupportedContracts struct {
	delay time.Duration
}

func (c delayedUnsupportedContracts) Supports(ContractVersion) bool {
	time.Sleep(c.delay)
	return false
}

func communityEnvelope(
	t *testing.T,
	proof contract.LeaseProof,
	generation int64,
	completeness contract.Completeness,
	postID string,
) contract.Envelope {
	t.Helper()
	payload, err := contract.MarshalPayloadV1(contract.CommunityPayloadV1{
		ChannelID: "UC_TEST",
		Posts:     []contract.CommunityPostV1{{PostID: postID, ChannelID: "UC_TEST"}},
		Coverage: contract.CommunityPageCoverageV1{
			ChannelID: "UC_TEST", MaxResults: 10, PageCount: 1, Exhausted: completeness == contract.CompletenessComplete,
		},
	})
	if err != nil {
		t.Fatalf("marshal community payload: %v", err)
	}
	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindCommunityPage,
		SubjectKey:         "UC_TEST",
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: generation,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       completeness,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              proof,
	})
	if err != nil {
		t.Fatalf("prepare community envelope: %v", err)
	}
	return envelope
}

func viewerEnvelope(
	t *testing.T,
	proof contract.LeaseProof,
	generation int64,
	count int64,
) contract.Envelope {
	return viewerEnvelopeFor(t, proof, generation, "video-1", count)
}

func viewerEnvelopeFor(
	t *testing.T,
	proof contract.LeaseProof,
	generation int64,
	subject string,
	count int64,
) contract.Envelope {
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
		Lease:              proof,
	})
	if err != nil {
		t.Fatalf("prepare viewer envelope: %v", err)
	}
	return envelope
}

func channelStatsEnvelope(
	t *testing.T,
	proof contract.LeaseProof,
	generation int64,
) contract.Envelope {
	t.Helper()
	count := int64(123)
	payload, err := contract.MarshalPayloadV1(contract.ChannelStatsV1{
		ChannelID:       "UC_TEST",
		SubscriberCount: &count,
		Coverage: contract.ChannelStatsCoverageV1{
			ChannelID: "UC_TEST", Fields: []string{"subscriber_count"},
		},
	})
	if err != nil {
		t.Fatalf("marshal channel stats payload: %v", err)
	}
	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider:           contract.ProviderYouTubeJS,
		ObservationKind:    contract.KindChannelStats,
		SubjectKey:         "UC_TEST",
		SchemaVersion:      contract.SchemaVersionV1,
		ContractGeneration: generation,
		ScheduledFor:       proof.ScheduledFor,
		ObservedAt:         proof.ScheduledFor.Add(time.Second),
		Completeness:       contract.CompletenessComplete,
		Continuity:         contract.ContinuityContiguous,
		Payload:            payload,
		CollectorInstance:  proof.OwnerInstance,
		Lease:              proof,
	})
	if err != nil {
		t.Fatalf("prepare channel stats envelope: %v", err)
	}
	return envelope
}

func channelProfileEnvelope(
	t *testing.T,
	proof contract.LeaseProof,
	generation int64,
) contract.Envelope {
	return channelProfileEnvelopeFor(t, proof, generation, "UC_TEST")
}

func channelProfileEnvelopeFor(
	t *testing.T,
	proof contract.LeaseProof,
	generation int64,
	subject string,
) contract.Envelope {
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
		Lease:              proof,
	})
	if err != nil {
		t.Fatalf("prepare channel profile envelope: %v", err)
	}
	return envelope
}

func checkpointForEnvelope(envelope contract.Envelope) CheckpointEntry {
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

func publishInput(envelope contract.Envelope) PublishBatchInput {
	return PublishBatchInput{
		Lease: envelope.Lease,
		Checkpoint: CheckpointUpdate{
			Entries: []CheckpointEntry{func() CheckpointEntry {
				entry := checkpointForEnvelope(envelope)
				entry.Cursor = json.RawMessage(`{"page":1}`)
				return entry
			}()},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{envelope},
	}
}

func claimOptions() ClaimOptions {
	return ClaimOptions{
		ConsumerName:  "youtube-community-processor",
		LeaseOwner:    "api-a",
		Kinds:         []contract.ObservationKind{contract.KindCommunityPage},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
}

func assertTableCount(t *testing.T, pool *pgxpool.Pool, table string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}
