package sourceobservation

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func TestPUB001SuccessfulCompletePreservesPriorFailureDiagnostic(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	prior := seedPriorLeaseFailure(t, ctx, pool, proof.JobKey)
	result, err := NewRepository(pool).PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].Outcome != PublishInserted || result.Results[0].Ordinal != 0 {
		t.Fatalf("result = %#v", result)
	}
	assertPublishSideEffects(t, pool, 1, 1, 1)
	got := readLeaseTerminal(t, ctx, pool, proof.JobKey)
	if got.state != "IDLE" || got.errorCode != "" {
		t.Fatalf("successful complete = %#v", got)
	}
	assertLeaseFailure(t, &got, &prior)
}

func TestPUB002DuplicateCompleteKeepsQueueIdentity(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	first, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if err != nil {
		t.Fatal(err)
	}
	reactivateLease(t, pool, &proof)
	second, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if err != nil {
		t.Fatal(err)
	}
	if second.Results[0].Outcome != PublishDuplicate || second.Results[0].ObservationID != first.Results[0].ObservationID {
		t.Fatalf("duplicate = %#v first=%#v", second.Results[0], first.Results[0])
	}
	assertTableCount(t, pool, "source_observations", 1)
	assertTableCount(t, pool, "source_observation_queue", 1)
	if got := readLeaseTerminal(t, ctx, pool, proof.JobKey); got.state != "IDLE" {
		t.Fatalf("duplicate complete state = %#v", got)
	}
}

func TestPUB003MixedCollisionCompletesWithDurableDiagnostic(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	baseID, baseKey, collision, independent, result := publishMixedCollisionBatch(t, ctx, pool, repo, &proof)
	assertMixedPublishResult(t, baseID, baseKey, collision, independent, result)
	assertMixedPersistence(t, ctx, pool, baseID, independent)
	got := readLeaseTerminal(t, ctx, pool, proof.JobKey)
	if got.state != "IDLE" || got.errorCode != string(contract.ErrorObservationCollision) ||
		got.failureCode != string(contract.ErrorObservationCollision) || got.failureClass != string(contract.ClassDataContract) ||
		got.failureDetail != observationCollisionDetail {
		t.Fatalf("collision complete diagnostic = %#v", got)
	}
}

func TestPublishBatchAndDeferRejectsEmptyObservations(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	deferInput := mustTestDeferInput(t, contract.ErrorCollectionFailed, contract.ClassTransient, "partial")
	_, err := NewRepository(pool).PublishBatchAndDefer(ctx, &PublishBatchInput{}, deferInput)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("empty PublishBatchAndDefer error = %v, want ErrInvalidEnvelope", err)
	}
	_, err = NewPublishRepository(pool).PublishBatchAndDefer(ctx, &PublishBatchInput{}, deferInput)
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("empty PublishRepository.PublishBatchAndDefer error = %v, want ErrInvalidEnvelope", err)
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)
}

func TestPUB004PartialOutputDefersAtomically(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	deferInput := mustTestDeferInput(t, contract.ErrorCollectionTimeout, contract.ClassTimeout, "partial collection timeout")
	scheduledFor := proof.ScheduledFor
	result, err := NewRepository(pool).PublishBatchAndDefer(ctx, publishInput(communityEnvelope(t, &proof, "post-1")), deferInput)
	if err != nil {
		t.Fatal(err)
	}
	if result.Results[0].Outcome != PublishInserted {
		t.Fatalf("partial result = %#v", result.Results[0])
	}
	assertPublishSideEffects(t, pool, 1, 1, 1)
	got := readLeaseTerminal(t, ctx, pool, proof.JobKey)
	if got.state != "DEFERRED" || got.errorCode != string(contract.ErrorCollectionTimeout) ||
		got.failureCode != string(contract.ErrorCollectionTimeout) || got.failureClass != string(contract.ClassTimeout) ||
		got.failureDetail != "partial collection timeout" || !got.scheduledFor.Equal(scheduledFor) || got.retryAt == nil {
		t.Fatalf("atomic defer = %#v", got)
	}
}

func TestPUB005PartialCollisionKeepsIndependentRowsAndPartialDiagnostic(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	base := communityEnvelope(t, &proof, "post-base")
	if _, err := repo.PublishBatch(ctx, publishInput(base)); err != nil {
		t.Fatal(err)
	}
	reactivateLease(t, pool, &proof)
	input := mixedCollisionInput(t, &proof)
	deferInput := mustTestDeferInput(t, contract.ErrorParserDrift, contract.ClassDataContract, "shorts tab drifted")
	result, err := repo.PublishBatchAndDefer(ctx, input, deferInput)
	if err != nil {
		t.Fatal(err)
	}
	if result.Results[0].Outcome != PublishCollision || result.Results[1].Outcome != PublishInserted {
		t.Fatalf("partial mixed result = %#v", result.Results)
	}
	assertMixedPersistence(t, ctx, pool, result.Results[0].ObservationID, &input.Observations[1])
	got := readLeaseTerminal(t, ctx, pool, proof.JobKey)
	if got.state != "DEFERRED" || got.failureCode != string(contract.ErrorParserDrift) || got.errorCode != string(contract.ErrorParserDrift) {
		t.Fatalf("partial collision diagnostic = %#v", got)
	}
}

func TestPUB006StaleFenceHasNoSideEffects(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	proof.FenceEpoch++
	_, err := NewRepository(pool).PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if !errors.Is(err, ErrCollectionFenceLost) {
		t.Fatalf("stale fence error = %v", err)
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)
	if got := readLeaseTerminal(t, ctx, pool, proof.JobKey); got.state != "ACTIVE" {
		t.Fatalf("stale fence mutated lease = %#v", got)
	}
}

func TestPUB007LeaseExpiredAfterPrepareBeforeTxHasNoSideEffects(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	input := publishInput(communityEnvelope(t, &proof, "post-1"))
	prepared, err := preparePublishBatch(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET lease_expires_at = clock_timestamp() - INTERVAL '1 second'
		WHERE job_key = $1
	`, proof.JobKey); err != nil {
		t.Fatal(err)
	}
	_, err = NewRepository(pool).runPreparedPublish(ctx, &prepared, NewRepository(pool).completePublishTerminal)
	if !errors.Is(err, ErrCollectionFenceLost) {
		t.Fatalf("expired-after-prepare error = %v", err)
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)
}

func TestPUB008StaleContractAndDisabledTargetHaveNoSideEffects(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	if _, err := pool.Exec(ctx, `
		UPDATE observation_contract_generations
		SET current_generation = 2
		WHERE provider = 'youtubejs' AND observation_kind = 'community_page'
	`); err != nil {
		t.Fatal(err)
	}
	_, err := NewRepository(pool).PublishBatchAndDefer(
		ctx,
		publishInput(communityEnvelope(t, &proof, "post-1")),
		mustTestDeferInput(t, contract.ErrorCollectionFailed, contract.ClassTransient, "unused"),
	)
	if !errors.Is(err, ErrStaleContract) {
		t.Fatalf("stale contract error = %v", err)
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)
}

func TestPUB009TerminalRowCountZeroRollsBackObservations(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	repo.publishFault = func(ctx context.Context, tx dbx.Tx, point publishFaultPoint) error {
		if point != faultBeforeTerminal {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE youtube_collection_job_leases
			SET lease_expires_at = clock_timestamp() - INTERVAL '1 second'
			WHERE job_key = $1
		`, proof.JobKey); err != nil {
			return err
		}
		return nil
	}
	_, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if !errors.Is(err, ErrCollectionFenceLost) {
		t.Fatalf("zero terminal rows error = %v", err)
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)
}

func TestPUB010InvalidPublishResultRollsBack(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	repo.rewritePublishResult = func(result PublishBatchResult) PublishBatchResult {
		result.Results = nil
		return result
	}
	_, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1")))
	if err == nil {
		t.Fatal("missing result must fail")
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)
}

func TestPUB011CallerMutationDuringTxUsesPreparedClone(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	input := publishInput(communityEnvelope(t, &proof, "post-1"))
	originalSHA := input.Observations[0].PayloadSHA256
	repo := NewRepository(pool)
	repo.publishFault = func(_ context.Context, _ dbx.Tx, point publishFaultPoint) error {
		if point != faultAfterFenceVerify {
			return nil
		}
		input.Observations[0].Payload[0] ^= 0xff
		input.Observations[0].SubjectKey = "mutated"
		return nil
	}
	if _, err := repo.PublishBatch(ctx, input); err != nil {
		t.Fatal(err)
	}
	var storedSHA string
	if err := pool.QueryRow(ctx, `SELECT payload_sha256 FROM source_observations`).Scan(&storedSHA); err != nil {
		t.Fatal(err)
	}
	if storedSHA != originalSHA {
		t.Fatalf("stored payload used mutated caller input: sha=%s want %s", storedSHA, originalSHA)
	}
}

func TestPUB012RetryAtAndDelayClampAgainstPostgresClock(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	scheduledFor := proof.ScheduledFor
	diagnostic, err := contract.NewFailureDiagnostic(contract.ErrorCooldown, contract.ClassCooldown, "cooldown")
	if err != nil {
		t.Fatal(err)
	}
	past, err := NewRetryAtSchedule(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	input, err := NewDeferCollectionInput(diagnostic, RetryBounds{Minimum: 200 * time.Millisecond, Maximum: time.Second}, past)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(pool).PublishBatchAndDefer(ctx, publishInput(communityEnvelope(t, &proof, "post-1")), input); err != nil {
		t.Fatal(err)
	}
	got := readLeaseTerminal(t, ctx, pool, proof.JobKey)
	assertClampedRetry(t, &got, scheduledFor)
	reactivateLease(t, pool, &proof)
	delay, err := NewRetryDelaySchedule(200 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	delayInput, err := NewDeferCollectionInput(diagnostic, RetryBounds{Minimum: 200 * time.Millisecond, Maximum: time.Second}, delay)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(pool).PublishBatchAndDefer(ctx, publishInput(communityEnvelope(t, &proof, "post-1")), delayInput); err != nil {
		t.Fatal(err)
	}
	got = readLeaseTerminal(t, ctx, pool, proof.JobKey)
	assertClampedRetry(t, &got, scheduledFor)
}

func assertClampedRetry(t *testing.T, got *leaseTerminalState, scheduledFor time.Time) {
	t.Helper()
	if !got.scheduledFor.Equal(scheduledFor) || got.retryAt == nil {
		t.Fatalf("retry clamp = %#v", got)
	}
	now := time.Now().UTC()
	if got.retryAt.Before(now.Add(50*time.Millisecond)) || got.retryAt.After(now.Add(1500*time.Millisecond)) {
		t.Fatalf("retry_not_before = %s not clamped to postgres min delay", got.retryAt)
	}
}

func TestPUB013InvalidTupleAndTerminalFaultRollBack(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	_, err := repo.PublishBatchAndDefer(ctx, publishInput(communityEnvelope(t, &proof, "post-1")), DeferCollectionInput{})
	if err == nil {
		t.Fatal("invalid defer input must fail before tx")
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)

	repo.publishFault = func(context.Context, dbx.Tx, publishFaultPoint) error {
		return errors.New("forced terminal fault")
	}
	_, err = repo.PublishBatchAndDefer(
		ctx,
		publishInput(communityEnvelope(t, &proof, "post-2")),
		mustTestDeferInput(t, contract.ErrorCollectionFailed, contract.ClassTransient, "unused"),
	)
	if err == nil {
		t.Fatal("terminal fault must fail")
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)

	var jobKey string
	err = pool.QueryRow(ctx, mustSQL("repository_job_defer_0082_82.sql"),
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		"not_a_code", "TRANSIENT", "detail", "DELAY", int64(200), time.Time{}, int64(100), int64(1000),
	).Scan(&jobKey)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("invalid SQL tuple error = %v, want no rows", err)
	}
	if got := readLeaseTerminal(t, ctx, pool, proof.JobKey); got.state != "ACTIVE" || got.failureCode != "" {
		t.Fatalf("invalid SQL tuple mutated lease = %#v", got)
	}
}

func TestPUB014AtomicDeferStoresConstructorDetailUnchanged(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	detail := "helper protocol reset"
	if _, err := NewRepository(pool).PublishBatchAndDefer(
		ctx,
		publishInput(communityEnvelope(t, &proof, "post-1")),
		mustTestDeferInput(t, contract.ErrorHelperProtocolMismatch, contract.ClassProtocol, detail),
	); err != nil {
		t.Fatal(err)
	}
	atomic := readLeaseTerminal(t, ctx, pool, proof.JobKey)
	if atomic.failureDetail != detail || atomic.failureCode != string(contract.ErrorHelperProtocolMismatch) ||
		atomic.failureClass != string(contract.ClassProtocol) {
		t.Fatalf("atomic defer diagnostic = %#v", atomic)
	}
}

func TestFaultBeforeCommitRollsBackCompleteAndObservations(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	proof := seedPublishLease(t, ctx, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, "UC_TEST", "community_collect")
	repo := NewRepository(pool)
	repo.publishFault = func(_ context.Context, _ dbx.Tx, point publishFaultPoint) error {
		if point == faultBeforeCommit {
			return errors.New("forced commit fault")
		}
		return nil
	}
	if _, err := repo.PublishBatch(ctx, publishInput(communityEnvelope(t, &proof, "post-1"))); err == nil {
		t.Fatal("commit fault must fail")
	}
	assertPublishSideEffects(t, pool, 0, 0, 0)
	if got := readLeaseTerminal(t, ctx, pool, proof.JobKey); got.state != "ACTIVE" {
		t.Fatalf("commit fault mutated lease = %#v", got)
	}
}

type leaseTerminalState struct {
	state         string
	errorCode     string
	failureCode   string
	failureClass  string
	failureDetail string
	failureAt     time.Time
	scheduledFor  time.Time
	retryAt       *time.Time
}

func readLeaseTerminal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobKey string) leaseTerminalState {
	t.Helper()
	var got leaseTerminalState
	var errorCode, failureCode, failureClass, failureDetail sql.NullString
	var failureAt sql.NullTime
	if err := pool.QueryRow(ctx, `
		SELECT slot_state, last_error_code, last_failure_code, last_failure_class, last_failure_detail,
		       last_failure_at, scheduled_for, retry_not_before
		FROM youtube_collection_job_leases
		WHERE job_key = $1
	`, jobKey).Scan(
		&got.state, &errorCode, &failureCode, &failureClass, &failureDetail, &failureAt, &got.scheduledFor, &got.retryAt,
	); err != nil {
		t.Fatal(err)
	}
	got.errorCode = errorCode.String
	got.failureCode = failureCode.String
	got.failureClass = failureClass.String
	got.failureDetail = failureDetail.String
	got.failureAt = failureAt.Time
	return got
}

func seedPriorLeaseFailure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobKey string) leaseTerminalState {
	t.Helper()
	prior := leaseTerminalState{
		failureCode:   string(contract.ErrorCollectionFailed),
		failureClass:  string(contract.ClassTransient),
		failureDetail: "prior failure",
	}
	if err := pool.QueryRow(ctx, `
		UPDATE youtube_collection_job_leases
		SET last_failure_code = $2,
		    last_failure_class = $3,
		    last_failure_detail = $4,
		    last_failure_at = clock_timestamp()
		WHERE job_key = $1
		RETURNING last_failure_at
	`, jobKey, prior.failureCode, prior.failureClass, prior.failureDetail).Scan(&prior.failureAt); err != nil {
		t.Fatal(err)
	}
	return prior
}

func assertLeaseFailure(t *testing.T, got, want *leaseTerminalState) {
	t.Helper()
	if got.failureCode != want.failureCode || got.failureClass != want.failureClass ||
		got.failureDetail != want.failureDetail || !got.failureAt.Equal(want.failureAt) {
		t.Fatalf("failure diagnostic = %#v, want %#v", got, want)
	}
}

func mixedCollisionInput(t *testing.T, proof *contract.LeaseProof) *PublishBatchInput {
	t.Helper()
	collision := communityEnvelope(t, proof, "post-collision")
	independent := independentCommunityEnvelope(t, proof)
	input := publishInput(collision)
	input.Observations = append(input.Observations, *independent)
	independentCheckpoint := checkpointForEnvelope(independent)
	independentCheckpoint.Cursor = []byte(`{"page":2}`)
	input.Checkpoint.Entries = append(input.Checkpoint.Entries, independentCheckpoint)
	return input
}
