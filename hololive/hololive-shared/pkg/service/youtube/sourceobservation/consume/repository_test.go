package consume

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation/observationtest"
)

const testAPILeaseOwner = "api-a"

func TestRetryRequiresUnexpiredClaim(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	repo := newTestRepository(pool)

	if _, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}

	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Claims) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}

	observation := batch.Claims[0]
	expireObservationClaim(t, pool, observation.ObservationID)

	_, err = repo.Retry(ctx, RetryInput{
		ObservationID: observation.ObservationID,
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
	`, observation.ObservationID).Scan(&status); err != nil {
		t.Fatalf("load queue after expired retry: %v", err)
	}

	if status != string(contract.StatusProcessing) {
		t.Fatalf("expired retry changed status to %s", status)
	}
}

func TestDeadLetterRequiresUnexpiredClaim(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	repo := newTestRepository(pool)

	if _, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}

	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Claims) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}

	observation := batch.Claims[0]
	expireObservationClaim(t, pool, observation.ObservationID)

	err = repo.DeadLetter(ctx, DeadLetterInput{
		ObservationID: observation.ObservationID,
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
	`, observation.ObservationID).Scan(&status); err != nil {
		t.Fatalf("load queue after expired dead letter: %v", err)
	}

	if status != string(contract.StatusProcessing) {
		t.Fatalf("expired dead letter changed status to %s", status)
	}
}

func beginQueueRowLocker(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64) pgx.Tx {
	t.Helper()

	locker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin queue locker: %v", err)
	}

	t.Cleanup(func() {
		if rollbackErr := locker.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback queue locker: %v", rollbackErr)
		}
	})

	var lockedID int64

	if err := locker.QueryRow(ctx, `
		SELECT observation_id
		FROM source_observation_queue
		WHERE observation_id = $1
		FOR UPDATE
	`, observationID).Scan(&lockedID); err != nil {
		t.Fatalf("lock queue row: %v", err)
	}

	if lockedID != observationID {
		t.Fatalf("locked observation id = %d, want %d", lockedID, observationID)
	}

	if _, err := locker.Exec(ctx, `
		UPDATE source_observation_queue
		SET lease_expires_at = clock_timestamp() - INTERVAL '1 second'
		WHERE observation_id = $1
	`, observationID); err != nil {
		t.Fatalf("expire locked claim: %v", err)
	}

	return locker
}

func startRetryInBackground(ctx context.Context, repo *testRepository, claim ClaimWork) <-chan error {
	retryResult := make(chan error, 1)
	started := make(chan struct{})

	go func() {
		close(started)

		_, retryErr := repo.Retry(ctx, RetryInput{
			ObservationID: claim.ObservationID,
			LeaseToken:    claim.LeaseToken,
			Delay:         time.Second,
			ErrorCode:     "provider_error",
			ErrorDetail:   "row-lock expiry regression",
		})
		retryResult <- retryErr
	}()

	<-started

	return retryResult
}

func requireQueueStatus(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64, want contract.Status) {
	t.Helper()

	var status string

	if err := pool.QueryRow(ctx, `
		SELECT status FROM source_observation_queue WHERE observation_id = $1
	`, observationID).Scan(&status); err != nil {
		t.Fatalf("load queue after retry: %v", err)
	}

	if status != string(want) {
		t.Fatalf("retry after expiry changed status to %s", status)
	}
}

func TestRetryExpiryAfterQueueRowLockWaitReturnsClaimLost(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	batch := publishAndClaimCommunityPost(ctx, t, pool, claimOptions())
	observation := batch.Claims[0]
	locker := beginQueueRowLocker(ctx, t, pool, observation.ObservationID)

	retryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)

	defer cancel()

	retryResult := startRetryInBackground(retryCtx, newTestRepository(pool), observation)

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

	requireQueueStatus(ctx, t, pool, observation.ObservationID, contract.StatusProcessing)
}

func TestClaimDoesNotStrandSupportedOldGenerationAfterCurrentBump(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	envelope := observationtest.CommunityEnvelope(t, &proof, "post-1")
	repo := newTestRepository(pool)

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

	if len(batch.Claims) != 1 {
		t.Fatalf("claimed work = %#v", batch.Claims)
	}

	if _, err := repo.Finalize(ctx, batch.Claims[0].Claim(batch.ConsumerName), func(_ context.Context, _ dbx.Tx, claimed *Observation) (ReconcileResult, error) {
		if claimed.ContractGeneration != 1 {
			t.Fatalf("locked contract generation = %d, want 1", claimed.ContractGeneration)
		}

		return ReconcileResult{}, nil
	}); err != nil {
		t.Fatalf("finalize old supported generation: %v", err)
	}
}

func TestFinalizeUnsupportedContractDeadLettersWithBoundedAudit(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	envelope := observationtest.CommunityEnvelope(t, &proof, "post-1")

	if _, err := newTestRepository(pool).PublishBatch(ctx, publishInput(envelope)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	repo := newTestRepositoryWithContracts(pool, StaticSupportedContracts{}, sourceobservation.InitialJobContracts())
	batch, err := repo.ClaimBatch(ctx, claimOptions())

	if err != nil || len(batch.Claims) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}

	observation := batch.Claims[0]
	result, err := repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ObservationID,
		LeaseToken:    observation.LeaseToken,
	}, func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error) {
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
	`, observation.ObservationID).Scan(&status, &code, &detail); err != nil {
		t.Fatalf("load DLQ: %v", err)
	}

	if status != "DEAD_LETTER" || code != "unsupported_contract" || len(detail) > maxErrorTextBytes {
		t.Fatalf("DLQ status=%s code=%s detail_bytes=%d", status, code, len(detail))
	}
}

func TestFinalizeFutureSourceEventFallsBackToScheduledSlot(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	envelope := observationtest.CommunityEnvelope(t, &proof, "post-1")
	future := time.Now().UTC().Add(time.Hour)

	envelope.SourceEventAt = &future

	prepared, err := contract.PrepareEnvelope(*envelope)
	if err != nil {
		t.Fatalf("prepare future source event: %v", err)
	}

	repo := newTestRepository(pool)
	if _, publishErr := repo.PublishBatch(ctx, publishInput(&prepared)); publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}

	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Claims) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}

	observation := batch.Claims[0]
	result, err := repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ObservationID,
		LeaseToken:    observation.LeaseToken,
	}, func(_ context.Context, _ dbx.Tx, claimed *Observation) (ReconcileResult, error) {
		if !claimed.SourceEventFallback || !claimed.EffectiveAt.Equal(claimed.ScheduledFor) {
			t.Fatalf("claimed clock = %#v", claimed)
		}

		return ReconcileResult{}, nil
	})

	if err != nil || !result.SourceEventFallback || !result.EffectiveAt.Equal(envelope.ScheduledFor) {
		t.Fatalf("finalize clock result=%#v err=%v", result, err)
	}
}

func publishAndClaimCommunityPost(ctx context.Context, t *testing.T, pool *pgxpool.Pool, options ClaimOptions) ClaimedBatch {
	t.Helper()

	proof := observationtest.SeedPublishLease(ctx, t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	if _, err := newTestRepository(pool).PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}

	batch, err := newTestRepository(pool).ClaimBatch(ctx, options)
	if err != nil || len(batch.Claims) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}

	return batch
}

func requireLiveLease(ctx context.Context, t *testing.T, pool *pgxpool.Pool, observationID int64, label string) {
	t.Helper()

	var leaseExpiresAt time.Time

	if err := pool.QueryRow(ctx, `
		SELECT lease_expires_at
		FROM source_observation_queue
		WHERE observation_id = $1
	`, observationID).Scan(&leaseExpiresAt); err != nil {
		t.Fatalf("load initial lease: %v", err)
	}

	if !leaseExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("%s did not begin with a valid lease: %s", label, leaseExpiresAt)
	}
}

func requireNoReconcileSideEffects(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	assertTableCount(t, pool, "youtube_community_posts", 0)
	assertTableCount(t, pool, "source_observation_applications", 0)
	assertTableCount(t, pool, "source_observation_consumer_offsets", 0)
}

func requireQueueClaimIntact(ctx context.Context, t *testing.T, pool *pgxpool.Pool, claim ClaimWork, label string) {
	t.Helper()

	var status, lastErrorCode, leaseToken string

	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(last_error_code, ''), COALESCE(lease_token, '')
		FROM source_observation_queue
		WHERE observation_id = $1
	`, claim.ObservationID).Scan(&status, &lastErrorCode, &leaseToken); err != nil {
		t.Fatalf("load queue after %s rollback: %v", label, err)
	}

	if status != string(contract.StatusProcessing) || lastErrorCode != "" || leaseToken != claim.LeaseToken {
		t.Fatalf("%s side effect committed: status=%s error=%s lease_token=%s", label, status, lastErrorCode, leaseToken)
	}
}

func expireClaimDuringReconcile(ctx context.Context, tx dbx.Tx, claimed *Observation) (ReconcileResult, error) {
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
}

func TestFinalizeLeaseExpiryAtTerminalUpdateRollsBackAllSideEffects(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	batch := publishAndClaimCommunityPost(ctx, t, pool, claimOptions())
	observation := batch.Claims[0]

	requireLiveLease(ctx, t, pool, observation.ObservationID, "claim")

	_, err := newTestRepository(pool).Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ObservationID,
		LeaseToken:    observation.LeaseToken,
	}, expireClaimDuringReconcile)
	if !errors.Is(err, ErrClaimLost) {
		t.Fatalf("expired terminal finalize error = %v, want ErrClaimLost", err)
	}

	requireNoReconcileSideEffects(t, pool)
	requireQueueClaimIntact(ctx, t, pool, observation, "queue")
}

func TestFinalizeUnsupportedContractExpiryAtDeadLetterRollsBackState(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	options := claimOptions()

	options.LeaseDuration = 2 * time.Second

	batch := publishAndClaimCommunityPost(ctx, t, pool, options)
	observation := batch.Claims[0]

	requireLiveLease(ctx, t, pool, observation.ObservationID, "DLQ regression")

	repo := newTestRepositoryWithContracts(
		pool,
		delayedUnsupportedContracts{delay: 2500 * time.Millisecond},
		sourceobservation.InitialJobContracts(),
	)

	_, err := repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ObservationID,
		LeaseToken:    observation.LeaseToken,
	}, func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error) {
		t.Fatal("unsupported contract must not invoke reconcile")

		return ReconcileResult{}, nil
	})
	if !errors.Is(err, ErrClaimLost) {
		t.Fatalf("expired unsupported-contract DLQ error = %v, want ErrClaimLost", err)
	}

	requireNoReconcileSideEffects(t, pool)
	requireQueueClaimIntact(ctx, t, pool, observation, "DLQ")
}

func TestReplayReactivatesTerminalQueueWithoutCopyingEvidence(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	proof := observationtest.SeedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	repo := newTestRepository(pool)

	if _, err := repo.PublishBatch(ctx, publishInput(observationtest.CommunityEnvelope(t, &proof, "post-1"))); err != nil {
		t.Fatalf("publish: %v", err)
	}

	batch, err := repo.ClaimBatch(ctx, claimOptions())
	if err != nil || len(batch.Claims) != 1 {
		t.Fatalf("claim: batch=%#v err=%v", batch, err)
	}

	observation := batch.Claims[0]
	if _, finalizeErr := repo.Finalize(ctx, Claim{
		ConsumerName:  batch.ConsumerName,
		ObservationID: observation.ObservationID,
		LeaseToken:    observation.LeaseToken,
	}, func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error) {
		return ReconcileResult{}, nil
	}); finalizeErr != nil {
		t.Fatalf("finalize: %v", finalizeErr)
	}

	replay, err := repo.RequestReplay(ctx, ReplayInput{
		ObservationID: observation.ObservationID,
		RequestedBy:   testReplayOperator,
		Reason:        "regression verification",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: result=%#v err=%v", replay, err)
	}

	assertTableCount(t, pool, "source_observations", 1)
	assertTableCount(t, pool, "source_observation_replay_requests", 1)

	var (
		status      string
		replayCount int
	)

	if err := pool.QueryRow(ctx, `
		SELECT status, replay_count FROM source_observation_queue WHERE observation_id = $1
	`, observation.ObservationID).Scan(&status, &replayCount); err != nil {
		t.Fatalf("load replayed queue: %v", err)
	}

	if status != "PENDING" || replayCount != 1 {
		t.Fatalf("status=%s replay_count=%d", status, replayCount)
	}
}

func TestRetentionSQLLocksCandidatesWithSkipLocked(t *testing.T) {
	queue := mustSQL("repository_retention_delete_queue_0076_76.sql")
	if !strings.Contains(queue, "FOR UPDATE OF candidate SKIP LOCKED") {
		t.Fatal("queue retention must lock candidates with SKIP LOCKED")
	}

	evidence := mustSQL("repository_retention_delete_evidence_0079_79.sql")
	if !strings.Contains(evidence, "delete_source_observation_retention_batch") || strings.Contains(evidence, "FOR UPDATE") {
		t.Fatal("evidence retention must use the restricted retention function")
	}

	replay := mustSQL("repository_retention_delete_replay_0078_78.sql")
	if !strings.Contains(replay, "NOT EXISTS") || !strings.Contains(replay, "source_observations") {
		t.Fatal("replay audit retention must keep rows while evidence remains")
	}

	applications := mustSQL("repository_retention_delete_applications_0083_83.sql")
	if !strings.Contains(applications, "delete_source_observation_application_retention_batch") ||
		strings.Contains(applications, "DELETE FROM") {
		t.Fatal("application retention must use the restricted retention function")
	}

	checkpoints := mustSQL("repository_retention_delete_checkpoints_0084_84.sql")
	if !strings.Contains(checkpoints, "delete_source_collection_checkpoint_retention_batch") ||
		strings.Contains(checkpoints, "DELETE FROM") {
		t.Fatal("checkpoint retention must use the restricted retention function")
	}
}

func TestClaimWorkStaysCompact(t *testing.T) {
	if size := reflect.TypeFor[ClaimWork]().Size(); size > 64 {
		t.Fatalf("ClaimWork size = %d bytes, want <= 64", size)
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

func expireObservationClaim(t *testing.T, pool *pgxpool.Pool, observationID int64) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
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

func checkpointForEnvelope(envelope *contract.Envelope) sourceobservation.CheckpointEntry {
	return sourceobservation.CheckpointEntry{
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

func publishInput(envelope *contract.Envelope) *sourceobservation.PublishBatchInput {
	return &sourceobservation.PublishBatchInput{
		Lease: envelope.Lease,
		Checkpoint: sourceobservation.CheckpointUpdate{
			Entries: []sourceobservation.CheckpointEntry{func() sourceobservation.CheckpointEntry {
				entry := checkpointForEnvelope(envelope)

				entry.Cursor = jsontext.Value(`{"page":1}`)

				return entry
			}()},
			CollectionLatency: time.Second,
		},
		Observations: []contract.Envelope{*envelope},
	}
}

func claimOptions() ClaimOptions {
	return ClaimOptions{
		ConsumerName:  "youtube-community-processor",
		LeaseOwner:    testAPILeaseOwner,
		Kinds:         []contract.ObservationKind{contract.KindCommunityPage},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
}
