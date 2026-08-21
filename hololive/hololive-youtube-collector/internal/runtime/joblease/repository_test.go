package joblease

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func testConfig() Config {
	return Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		RenewTimeout: 50 * time.Millisecond, DBTimeout: 100 * time.Millisecond, CleanupTimeout: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 10, WorkerCount: 2, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
}

func TestConfigRequiresBoundedLeaseAndRuntimeBudgets(t *testing.T) {
	config := testConfig()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.RenewInterval = config.LeaseTTL / 2
	if !errors.Is(config.Validate(), ErrInvalidConfig) {
		t.Fatalf("renew interval validation error = %v", config.Validate())
	}
	config = testConfig()
	config.CleanupTimeout = 0
	if !errors.Is(config.Validate(), ErrInvalidConfig) {
		t.Fatalf("lease budget validation error = %v", config.Validate())
	}
}

func TestAcquireIncrementsEpochAndTakeoverPreservesScheduledSlot(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob()

	first, err := repository.Acquire(ctx, spec, "collector-a")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if first.Proof().FenceEpoch != 1 || first.Proof().ProjectionGeneration <= 0 {
		t.Fatalf("first proof = %#v", first.Proof())
	}
	if _, err := repository.Acquire(ctx, spec, "collector-b"); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("concurrent acquire error = %v", err)
	}
	if _, err := pool.Exec(ctx, mustTestSQL("expire_lease.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}
	second, err := repository.Acquire(ctx, spec, "collector-b")
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if second.Proof().FenceEpoch != 2 || !second.Proof().ScheduledFor.Equal(first.Proof().ScheduledFor) {
		t.Fatalf("takeover proof = %#v, first = %#v", second.Proof(), first.Proof())
	}
	if err := first.Renew(ctx); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("stale renew error = %v", err)
	}
	if err := second.Renew(ctx); err != nil {
		t.Fatalf("current renew: %v", err)
	}
}

func TestProjectionLockUsesRestrictedRoleFunction(t *testing.T) {
	query := mustSQL("repository_projection_lock_0144_05.sql")
	if !strings.Contains(query, "lock_youtube_collection_projection") {
		t.Fatal("projection lock query must use the restricted-role lock function")
	}
	if strings.Contains(strings.ToUpper(query), "FOR SHARE") {
		t.Fatal("projection lock query must not lock the table directly")
	}
}

func TestOnlyOneGlobalHolderIsActive(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"global:hololive-schedule", contract.KindSchedule, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := JobSpec{
		JobKey: "collector:hololive_official:official_schedule:global", Provider: contract.ProviderHololiveOfficial,
		Class: "GLOBAL", CollectionJobKind: "official_schedule",
		SubjectKey: "global:hololive-schedule", PollInterval: time.Minute,
	}
	if _, err := repository.Acquire(ctx, &spec, "collector-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Acquire(ctx, &spec, "collector-b"); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("second global acquire error = %v", err)
	}
	var active int
	if err := pool.QueryRow(ctx, mustTestSQL("active_lease_count.sql"), spec.JobKey).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active global holders = %d", active)
	}
}

func TestAcquireDoesNotBlockOnLeaseRowLockHeldByFinishingHolder(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob()

	lease, err := repository.Acquire(ctx, spec, "collector-a")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	proof := lease.Proof()
	holdActiveLeaseRow(t, pool, &proof)

	acquireCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err = repository.Acquire(acquireCtx, spec, "collector-b")
	elapsed := time.Since(started)
	if !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("contended acquire error = %v after %s, want ErrNotAcquired", err, elapsed)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("contended acquire elapsed = %s, want rejection without waiting on the row lock", elapsed)
	}
}

func TestConcurrentExpiredTakeoverProducesExactlyOneHolder(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob()

	if _, err := repository.Acquire(ctx, spec, "collector-a"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := pool.Exec(ctx, mustTestSQL("expire_lease.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}

	const contenders = 4
	var acquired atomic.Int64
	var group sync.WaitGroup
	errs := make([]error, contenders)
	start := make(chan struct{})
	for i := range contenders {
		group.Go(func() {
			<-start
			lease, err := repository.Acquire(ctx, spec, "collector-"+strconv.Itoa(i))
			if err == nil {
				acquired.Add(1)
				if lease.Proof().FenceEpoch != 2 {
					errs[i] = fmt.Errorf("winner fence epoch = %d, want 2", lease.Proof().FenceEpoch)
				}
				return
			}
			if !errors.Is(err, ErrNotAcquired) {
				errs[i] = err
			}
		})
	}
	close(start)
	group.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("contender %d: %v", i, err)
		}
	}
	if acquired.Load() != 1 {
		t.Fatalf("successful takeovers = %d, want 1", acquired.Load())
	}
	var active int
	if err := pool.QueryRow(ctx, mustTestSQL("active_lease_count.sql"), spec.JobKey).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active holders = %d, want 1", active)
	}
}

func TestMismatchedIdentityAcquireRollsBackTheLeaseUpdate(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{"channel:a", contract.KindCommunityPage, time.Minute, true},
		{"channel:b", contract.KindCommunityPage, time.Minute, true},
	})
	repository := newTestRepository(t, pool)
	spec := communityJob()

	if _, err := repository.Acquire(ctx, spec, "collector-a"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := pool.Exec(ctx, mustTestSQL("expire_lease.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}

	impostor := *spec
	impostor.SubjectKey = "channel:b"
	if _, err := repository.Acquire(ctx, &impostor, "collector-b"); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("mismatched identity acquire error = %v, want ErrInvalidJob", err)
	}

	takeover, err := repository.Acquire(ctx, spec, "collector-c")
	if err != nil {
		t.Fatalf("takeover after rejected identity: %v", err)
	}
	if takeover.Proof().FenceEpoch != 2 {
		t.Fatalf("fence epoch = %d, want 2 because the rejected acquire must roll back", takeover.Proof().FenceEpoch)
	}
}

func holdActiveLeaseRow(t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("release held lease row: %v", err)
		}
	})
	var failureCode, failureClass, failureDetail *string
	var failureAt *time.Time
	err = tx.QueryRow(
		ctx,
		mustSQL("repository_lease_failure_lock_0144_14.sql"),
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
	).Scan(&failureCode, &failureClass, &failureDetail, &failureAt)
	if err != nil {
		t.Fatalf("hold active lease row: %v", err)
	}
}

func TestYouTubeJSChannelCandidatesKeepLiveAndMetadataCadencesSeparate(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{"UC_A", contract.KindLiveSnapshot, 2 * time.Minute, true},
		{"UC_A", contract.KindChannelStats, 6 * time.Hour, true},
		{"UC_A", contract.KindChannelProfile, 6 * time.Hour, true},
		{"UC_A", contract.KindChannelPhoto, 6 * time.Hour, true},
	})
	repository := newTestRepository(t, pool)
	live := candidateJobs(t, repository, contract.ProviderYouTubeJS, "youtubejs_channel_live", 1)
	if len(live) != 1 || live[0].PollInterval != 2*time.Minute {
		t.Fatalf("live candidates = %#v", live)
	}
	metadata := candidateJobs(t, repository, contract.ProviderYouTubeJS, "youtubejs_channel_metadata", 1)
	if len(metadata) != 1 || metadata[0].PollInterval != 6*time.Hour {
		t.Fatalf("metadata candidates = %#v", metadata)
	}
	if _, err := repository.Acquire(ctx, &live[0], "collector-a"); err != nil {
		t.Fatalf("acquire youtubejs live candidate: %v", err)
	}
	if _, err := repository.Acquire(ctx, &metadata[0], "collector-a"); err != nil {
		t.Fatalf("acquire youtubejs metadata candidate: %v", err)
	}
}

func TestHolodexCandidatesKeepLiveScheduleAndMetadataCadencesSeparate(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{"UC_A", contract.KindLiveSnapshot, 2 * time.Minute, true},
		{"video-A", contract.KindViewerSample, 2 * time.Minute, true},
		{"UC_A", contract.KindChannelStats, 6 * time.Hour, true},
		{"UC_A", contract.KindChannelPhoto, 6 * time.Hour, true},
		{"global:hololive-schedule", contract.KindSchedule, 5 * time.Minute, true},
	})
	repository := newTestRepository(t, pool)
	tests := []struct {
		jobKind  string
		interval time.Duration
	}{
		{"holodex_live", 2 * time.Minute},
		{"holodex_schedule", 5 * time.Minute},
		{"holodex_metadata", 6 * time.Hour},
	}
	for _, tt := range tests {
		candidates := candidateJobs(t, repository, contract.ProviderHolodex, tt.jobKind, 1)
		if len(candidates) != 1 || candidates[0].PollInterval != tt.interval {
			t.Fatalf("%s candidates = %#v", tt.jobKind, candidates)
		}
		if _, err := repository.Acquire(ctx, &candidates[0], "collector-a"); err != nil {
			t.Fatalf("acquire %s candidate: %v", tt.jobKind, err)
		}
	}
}

func TestCandidatesEventuallyIncludeSubjectsBeyondAcquisitionBatch(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{"channel:a", contract.KindCommunityPage, time.Minute, true},
		{"channel:b", contract.KindCommunityPage, time.Minute, true},
		{"channel:c", contract.KindCommunityPage, time.Minute, true},
	})
	repository := newTestRepository(t, pool)
	first := candidateJobs(t, repository, contract.ProviderYouTubeJS, "community_collect", 2)
	if len(first) != 2 {
		t.Fatalf("first candidates = %#v", first)
	}
	for i := range first {
		lease, err := repository.Acquire(ctx, &first[i], "collector-a")
		if err != nil {
			t.Fatalf("acquire first candidate %d: %v", i, err)
		}
		if err := lease.Complete(ctx); err != nil {
			t.Fatalf("complete first candidate %d: %v", i, err)
		}
	}
	second := candidateJobs(t, repository, contract.ProviderYouTubeJS, "community_collect", 2)
	if len(second) != 1 || second[0].SubjectKey != "channel:c" {
		t.Fatalf("second candidates = %#v", second)
	}
}

func TestIdleAcquisitionCoalescesLongOutage(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob()
	lease, err := repository.Acquire(ctx, spec, "collector-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, mustTestSQL("make_lease_overdue.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}
	coalesced, err := repository.Acquire(ctx, spec, "collector-b")
	if err != nil {
		t.Fatal(err)
	}
	var recent bool
	if err := pool.QueryRow(ctx, mustTestSQL("scheduled_for_is_recent.sql"), coalesced.Proof().ScheduledFor).Scan(&recent); err != nil {
		t.Fatal(err)
	}
	if !recent {
		t.Fatalf("scheduled slot was not coalesced: %s", coalesced.Proof().ScheduledFor)
	}
}

func TestDeferAndReleasePreserveScheduledSlot(t *testing.T) {
	for _, action := range []string{"defer", "release"} {
		t.Run(action, func(t *testing.T) {
			assertActionPreservesScheduledSlot(t, action)
		})
	}
}

func assertActionPreservesScheduledSlot(t *testing.T, action string) {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob()
	first, err := repository.Acquire(ctx, spec, "collector-a")
	if err != nil {
		t.Fatal(err)
	}
	if action == "defer" {
		err = first.Defer(ctx, time.Now().UTC().Add(500*time.Millisecond), string(contract.ErrorCollectionTimeout), string(contract.ClassTimeout), "provider timed out")
	} else {
		err = first.Release(ctx, ReleaseShutdown)
	}
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	if _, err := pool.Exec(ctx, mustTestSQL("make_retry_due.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}
	second, err := repository.Acquire(ctx, spec, "collector-b")
	if err != nil {
		t.Fatalf("reacquire after %s: %v", action, err)
	}
	if !second.Proof().ScheduledFor.Equal(first.Proof().ScheduledFor) || second.Proof().FenceEpoch != first.Proof().FenceEpoch+1 {
		t.Fatalf("slot changed after %s: first=%#v second=%#v", action, first.Proof(), second.Proof())
	}
}

func TestDeferClampsShortRetryAgainstDatabaseClock(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	lease, err := repository.Acquire(ctx, communityJob(), "collector-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Defer(ctx, time.Now().UTC(), string(contract.ErrorCollectionFailed), string(contract.ClassTransient), "provider rate limited"); err != nil {
		t.Fatalf("defer short retry: %v", err)
	}
	var state string
	var retryAt time.Time
	if err := pool.QueryRow(ctx, mustTestSQL("lease_deferred_state.sql"), lease.Proof().JobKey).Scan(&state, &retryAt); err != nil {
		t.Fatal(err)
	}
	if state != "DEFERRED" || retryAt.Before(time.Now().UTC()) || retryAt.After(time.Now().UTC().Add(repository.config.MaxRetryDelay)) {
		t.Fatalf("deferred state=%s retry_at=%s", state, retryAt)
	}
}

func TestDeferAndCompleteRetainFailureDiagnostics(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	first, err := repository.Acquire(ctx, communityJob(), "collector-a")
	if err != nil {
		t.Fatal(err)
	}
	rawDetail := "youtube.js helper: Authorization: Bearer secret-value"
	detail := collecterr.SanitizeDetail(rawDetail)
	if err := first.Defer(ctx, time.Now().UTC().Add(500*time.Millisecond), string(contract.ErrorCollectionFailed), string(contract.ClassTransient), rawDetail); err != nil {
		t.Fatalf("defer: %v", err)
	}

	var currentCode, failureCode, failureClass, failureDetail string
	var failureAtSet bool
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(last_error_code, ''), last_failure_code, last_failure_class, last_failure_detail,
		       last_failure_at IS NOT NULL
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, first.Proof().JobKey).Scan(&currentCode, &failureCode, &failureClass, &failureDetail, &failureAtSet); err != nil {
		t.Fatal(err)
	}
	if currentCode != string(contract.ErrorCollectionFailed) || failureCode != string(contract.ErrorCollectionFailed) || failureClass != string(contract.ClassTransient) || !failureAtSet {
		t.Fatalf("deferred diagnostics = code:%q failure_code:%q class:%q at:%t", currentCode, failureCode, failureClass, failureAtSet)
	}
	if strings.Contains(failureDetail, "secret-value") || failureDetail != detail {
		t.Fatalf("failure detail = %q, want redacted %q", failureDetail, detail)
	}

	if _, err := pool.Exec(ctx, mustTestSQL("make_retry_due.sql"), first.Proof().JobKey); err != nil {
		t.Fatal(err)
	}
	second, err := repository.Acquire(ctx, communityJob(), "collector-b")
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	if err := second.Complete(ctx); err != nil {
		t.Fatalf("complete: %v", err)
	}

	var retryCleared bool
	var retainedCode, retainedClass, retainedDetail string
	var retainedAtSet bool
	if err := pool.QueryRow(ctx, `
		SELECT last_error_code IS NULL, last_failure_code, last_failure_class, last_failure_detail,
		       last_failure_at IS NOT NULL
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, first.Proof().JobKey).Scan(&retryCleared, &retainedCode, &retainedClass, &retainedDetail, &retainedAtSet); err != nil {
		t.Fatal(err)
	}
	if !retryCleared || retainedCode != failureCode || retainedClass != failureClass || retainedDetail != failureDetail || !retainedAtSet {
		t.Fatalf("retained diagnostics = retry_cleared:%t code:%q class:%q detail:%q at:%t", retryCleared, retainedCode, retainedClass, retainedDetail, retainedAtSet)
	}
}

func TestLegacyDeferBackfillsDiagnosticsWithoutOverwritingNewWriter(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob()

	first := mustAcquireLease(t, ctx, repository, spec, "collector-a")
	mustDeferLease(t, ctx, first, string(contract.ErrorCollectionFailed), string(contract.ClassTransient), "first detail")
	stale := readFailureDiagnostics(t, ctx, pool, spec.JobKey)

	makeRetryDue(t, ctx, pool, spec.JobKey)
	second := mustAcquireLease(t, ctx, repository, spec, "collector-b")
	legacyDefer(t, ctx, pool, repository, second, "legacy_failure")
	legacy := readFailureDiagnostics(t, ctx, pool, spec.JobKey)
	if legacy.code != "legacy_failure" || legacy.class != "legacy_collector" || legacy.detail != "legacy_collector" {
		t.Fatalf("legacy diagnostics = code:%q class:%q detail:%q", legacy.code, legacy.class, legacy.detail)
	}
	if !legacy.at.After(stale.at) {
		t.Fatalf("legacy failure timestamp = %s, stale timestamp = %s", legacy.at, stale.at)
	}

	makeRetryDue(t, ctx, pool, spec.JobKey)
	third := mustAcquireLease(t, ctx, repository, spec, "collector-c")
	mustDeferLease(t, ctx, third, string(contract.ErrorCollectionTimeout), string(contract.ClassTimeout), "second detail")
	current := readFailureDiagnostics(t, ctx, pool, spec.JobKey)
	if current.code != string(contract.ErrorCollectionTimeout) || current.class != string(contract.ClassTimeout) || current.detail != "second detail" {
		t.Fatalf("new-writer diagnostics = code:%q class:%q detail:%q", current.code, current.class, current.detail)
	}
	if !current.at.After(legacy.at) {
		t.Fatalf("new-writer timestamp = %s, legacy timestamp = %s", current.at, legacy.at)
	}

	makeRetryDue(t, ctx, pool, spec.JobKey)
	fourth := mustAcquireLease(t, ctx, repository, spec, "collector-d")
	if err := fourth.Complete(ctx); err != nil {
		t.Fatalf("complete after new-writer defer: %v", err)
	}
	assertFailureDiagnostics(t, ctx, pool, spec.JobKey, current.code, current.class, current.detail, current.at)
	if _, err := pool.Exec(ctx, mustTestSQL("make_lease_overdue.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Acquire(ctx, spec, "collector-e"); err != nil {
		t.Fatalf("acquire after legacy-compatible complete: %v", err)
	}
	assertFailureDiagnostics(t, ctx, pool, spec.JobKey, current.code, current.class, current.detail, current.at)
}

func TestAcquirePreservesLegacyDeferredFailureDuringMigrationBackfill(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob()

	first := mustAcquireLease(t, ctx, repository, spec, "collector-a")
	if err := first.Complete(ctx); err != nil {
		t.Fatalf("complete initial lease: %v", err)
	}
	var failureAt time.Time
	if err := pool.QueryRow(ctx, `
		UPDATE youtube_collection_job_leases
		SET slot_state = 'DEFERRED',
		    retry_not_before = clock_timestamp() - INTERVAL '1 second',
		    last_error_code = 'pretrigger_failure',
		    updated_at = clock_timestamp() - INTERVAL '1 minute'
		WHERE job_key = $1
		RETURNING updated_at
	`, spec.JobKey).Scan(&failureAt); err != nil {
		t.Fatalf("seed legacy deferred failure: %v", err)
	}

	mustAcquireLease(t, ctx, repository, spec, "collector-b")
	assertFailureDiagnostics(t, ctx, pool, spec.JobKey,
		"pretrigger_failure", "legacy_collector", "legacy_collector", failureAt)
}

type failureDiagnostics struct {
	code   string
	class  string
	detail string
	at     time.Time
}

func readFailureDiagnostics(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobKey string) failureDiagnostics {
	t.Helper()
	var diagnostics failureDiagnostics
	if err := pool.QueryRow(ctx, `
		SELECT last_failure_code, last_failure_class, last_failure_detail, last_failure_at
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, jobKey).Scan(&diagnostics.code, &diagnostics.class, &diagnostics.detail, &diagnostics.at); err != nil {
		t.Fatal(err)
	}
	return diagnostics
}

func makeRetryDue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobKey string) {
	t.Helper()
	if _, err := pool.Exec(ctx, mustTestSQL("make_retry_due.sql"), jobKey); err != nil {
		t.Fatal(err)
	}
}

func mustAcquireLease(t *testing.T, ctx context.Context, repository *Repository, spec *JobSpec, owner string) *JobLease {
	t.Helper()
	lease, err := repository.Acquire(ctx, spec, owner)
	if err != nil {
		t.Fatalf("acquire lease for %s: %v", owner, err)
	}
	return lease
}

func mustDeferLease(t *testing.T, ctx context.Context, lease *JobLease, code, class, detail string) {
	t.Helper()
	if err := lease.Defer(ctx, time.Now().UTC().Add(500*time.Millisecond), code, class, detail); err != nil {
		t.Fatalf("defer lease: %v", err)
	}
}

func legacyDefer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *Repository, lease *JobLease, code string) {
	t.Helper()
	const query = `UPDATE youtube_collection_job_leases
SET slot_state = 'DEFERRED', owner_instance = NULL, lease_expires_at = NULL,
    retry_not_before = LEAST(
        GREATEST($6, statement_timestamp() + ($8::bigint * INTERVAL '1 millisecond')),
        statement_timestamp() + ($9::bigint * INTERVAL '1 millisecond')
    ),
    last_error_code = $7, updated_at = clock_timestamp()
WHERE job_key = $1 AND owner_instance = $2 AND fence_epoch = $3
  AND projection_generation = $4 AND scheduled_for = $5
  AND slot_state = 'ACTIVE' AND lease_expires_at > clock_timestamp()
RETURNING job_key`
	proof := lease.Proof()
	var jobKey string
	if err := pool.QueryRow(ctx, query,
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		time.Now().UTC().Add(500*time.Millisecond), code,
		repository.config.MinRetryDelay.Milliseconds(), repository.config.MaxRetryDelay.Milliseconds(),
	).Scan(&jobKey); err != nil {
		t.Fatalf("legacy defer: %v", err)
	}
	if jobKey != proof.JobKey {
		t.Fatalf("legacy defer job key = %q, want %q", jobKey, proof.JobKey)
	}
}

func TestReleasePreservesExistingFailureDiagnostics(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob()

	first, err := repository.Acquire(ctx, spec, "collector-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Defer(ctx, time.Now().UTC().Add(500*time.Millisecond), string(contract.ErrorCollectionFailed), string(contract.ClassTransient), "provider detail"); err != nil {
		t.Fatalf("record provider failure: %v", err)
	}
	var failureCode, failureClass, failureDetail string
	var failureAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT last_failure_code, last_failure_class, last_failure_detail, last_failure_at
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey).Scan(&failureCode, &failureClass, &failureDetail, &failureAt); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, mustTestSQL("make_retry_due.sql"), spec.JobKey); err != nil {
		t.Fatal(err)
	}
	second, err := repository.Acquire(ctx, spec, "collector-b")
	if err != nil {
		t.Fatalf("reacquire for release: %v", err)
	}
	if err := second.Release(ctx, ReleaseShutdown); err != nil {
		t.Fatalf("release after provider failure: %v", err)
	}

	var retryCode string
	if err := pool.QueryRow(ctx, `
		SELECT last_error_code FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey).Scan(&retryCode); err != nil {
		t.Fatal(err)
	}
	if retryCode != "shutdown_release" {
		t.Fatalf("release retry code = %q, want shutdown_release", retryCode)
	}
	assertFailureDiagnostics(t, ctx, pool, spec.JobKey, failureCode, failureClass, failureDetail, failureAt)
}

func assertFailureDiagnostics(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobKey, wantCode, wantClass, wantDetail string, wantAt time.Time) {
	t.Helper()
	var gotCode, gotClass, gotDetail string
	var gotAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT last_failure_code, last_failure_class, last_failure_detail, last_failure_at
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, jobKey).Scan(&gotCode, &gotClass, &gotDetail, &gotAt); err != nil {
		t.Fatal(err)
	}
	if gotCode != wantCode || gotClass != wantClass || gotDetail != wantDetail || !gotAt.Equal(wantAt) {
		t.Fatalf("failure diagnostics = code:%q class:%q detail:%q at:%s, want code:%q class:%q detail:%q at:%s", gotCode, gotClass, gotDetail, gotAt, wantCode, wantClass, wantDetail, wantAt)
	}
}

func TestDeferRejectsInvalidDiagnosticBounds(t *testing.T) {
	lease := &JobLease{}
	err := lease.Defer(context.Background(), time.Now().UTC().Add(time.Second), string(contract.ErrorCollectionFailed), string(contract.ClassTransient), strings.Repeat("x", collecterr.MaxDetailBytes+1))
	if !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("oversized diagnostic error = %v, want ErrInvalidJob", err)
	}
	if err := lease.Defer(context.Background(), time.Now().UTC().Add(time.Second), "provider_failed", "HelperError", "detail"); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("invalid tuple error = %v, want ErrInvalidJob", err)
	}
}

func TestProjectionExpiryBlocksAcquisition(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	if _, err := pool.Exec(ctx, mustTestSQL("expire_projection.sql"), generation); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestRepository(t, pool).Acquire(ctx, communityJob(), "collector-a"); !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("expired projection acquire error = %v", err)
	}
}

func TestLoadExactTargetSnapshotReturnsOnlyLeasedSubjectInOneQuery(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{
		{"channel:a", contract.KindCommunityPage, time.Minute, true},
		{"channel:b", contract.KindCommunityPage, time.Minute, true},
	})
	repository, queryCount := newCountingRepository(t, pool)
	spec := *communityJob()
	proof := snapshotProof(&spec, generation)
	job, _ := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: spec.Provider, Kind: sourceobservation.JobKind(spec.CollectionJobKind),
	})
	snapshot, err := repository.LoadTargetSnapshot(ctx, &proof, &spec, job, 10)
	if err != nil {
		t.Fatal(err)
	}
	roster, err := snapshot.Roster(contract.KindCommunityPage)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 1 || roster[0] != spec.SubjectKey {
		t.Fatalf("exact snapshot roster = %#v", roster)
	}
	if queryCount.Load() != 1 {
		t.Fatalf("exact snapshot query count = %d", queryCount.Load())
	}
}

func TestLoadProjectionTargetSnapshotPreservesEmptyAndEnforcesCap(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{
		{"UC_A", contract.KindLiveSnapshot, time.Minute, true},
		{"UC_B", contract.KindLiveSnapshot, time.Minute, true},
	})
	repository, queryCount := newCountingRepository(t, pool)
	job, _ := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: contract.ProviderHolodex, Kind: "holodex_schedule",
	})
	spec := JobSpec{
		JobKey: "collector:holodex:holodex_schedule:global", Provider: contract.ProviderHolodex,
		Class: "GLOBAL", CollectionJobKind: "holodex_schedule", SubjectKey: job.LeaseSubject(), PollInterval: time.Minute,
	}
	proof := snapshotProof(&spec, generation)
	snapshot, err := repository.LoadTargetSnapshot(ctx, &proof, &spec, job, 2)
	if err != nil {
		t.Fatal(err)
	}
	live, err := snapshot.Roster(contract.KindLiveSnapshot)
	if err != nil || len(live) != 2 {
		t.Fatalf("live roster = %#v, %v", live, err)
	}
	schedule, err := snapshot.Roster(contract.KindSchedule)
	if err != nil || schedule == nil || len(schedule) != 0 {
		t.Fatalf("schedule sentinel = %#v, %v", schedule, err)
	}
	if queryCount.Load() != 1 {
		t.Fatalf("projection snapshot query count = %d", queryCount.Load())
	}
	if _, err := repository.LoadTargetSnapshot(ctx, &proof, &spec, job, 1); collecterr.CodeOf(err) != collecterr.TargetRosterTooLarge {
		t.Fatalf("roster cap error = %v", err)
	}
}

func TestLoadTargetSnapshotRejectsStaleProjection(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	if _, err := pool.Exec(ctx, mustTestSQL("expire_projection.sql"), generation); err != nil {
		t.Fatal(err)
	}
	repository := newTestRepository(t, pool)
	spec := *communityJob()
	job, _ := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: spec.Provider, Kind: sourceobservation.JobKind(spec.CollectionJobKind),
	})
	proof := snapshotProof(&spec, generation)
	if _, err := repository.LoadTargetSnapshot(ctx, &proof, &spec, job, 10); !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("stale snapshot error = %v", err)
	}
}

func TestCompleteCurrentFailsClosedAfterTargetDisabledOrProjectionStale(t *testing.T) {
	for _, scenario := range []string{"disabled", "stale"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pool := dbtest.NewPool(t)
			generation := seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
			repository := newTestRepository(t, pool)
			lease, err := repository.Acquire(ctx, communityJob(), "collector-a")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "disabled" {
				if _, err := pool.Exec(ctx, `
					UPDATE youtube_collection_targets
					SET enabled = FALSE
					WHERE projection_generation = $1
				`, generation); err != nil {
					t.Fatal(err)
				}
			} else if _, err := pool.Exec(ctx, mustTestSQL("expire_projection.sql"), generation); err != nil {
				t.Fatal(err)
			}
			err = lease.CompleteCurrent(ctx)
			if scenario == "disabled" && !errors.Is(err, ErrTargetDisabled) {
				t.Fatalf("disabled complete error = %v", err)
			}
			if scenario == "stale" && !errors.Is(err, ErrProjectionStale) {
				t.Fatalf("stale complete error = %v", err)
			}
		})
	}
}

func snapshotProof(spec *JobSpec, generation int64) contract.LeaseProof {
	return contract.LeaseProof{
		JobKey: spec.JobKey, CollectionJobKind: spec.CollectionJobKind, OwnerInstance: "collector-a",
		FenceEpoch: 1, ProjectionGeneration: generation,
		ScheduledFor: time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
	}
}

type queryCounter struct {
	count atomic.Int32
}

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.count.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *queryCounter) Load() int32 {
	return c.count.Load()
}

func newCountingRepository(t *testing.T, pool *pgxpool.Pool) (*Repository, *queryCounter) {
	t.Helper()
	config := pool.Config().Copy()
	counter := &queryCounter{}
	config.ConnConfig.Tracer = counter
	tracedPool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tracedPool.Close)
	leaseConfig := testConfig()
	repository, err := NewRepository(tracedPool, &leaseConfig)
	if err != nil {
		t.Fatal(err)
	}
	return repository, counter
}

func TestYouTubeSubjectJobsDistributeWithoutDuplicateAcquisition(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{"channel:a", contract.KindCommunityPage, time.Minute, true},
		{"channel:b", contract.KindCommunityPage, time.Minute, true},
	})
	repository := newTestRepository(t, pool)
	candidates := candidateJobs(t, repository, contract.ProviderYouTubeJS, "community_collect", 10)
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d", len(candidates))
	}
	first, err := repository.Acquire(ctx, &candidates[0], "collector-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Acquire(ctx, &candidates[0], "collector-b"); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("duplicate acquisition error = %v", err)
	}
	second, err := repository.Acquire(ctx, &candidates[1], "collector-b")
	if err != nil {
		t.Fatal(err)
	}
	if first.Proof().JobKey == second.Proof().JobKey {
		t.Fatal("distributed subject jobs used the same job key")
	}
}

type fakeLease struct {
	proof        contract.LeaseProof
	renewCalls   atomic.Int32
	releaseCalls atomic.Int32
	lastRelease  ReleaseReason
	renewErr     error
}

func (l *fakeLease) Proof() contract.LeaseProof { return l.proof }
func (l *fakeLease) Renew(context.Context) error {
	l.renewCalls.Add(1)
	if l.renewErr != nil {
		return l.renewErr
	}
	return ErrFenceLost
}
func (l *fakeLease) Complete(context.Context) error                                 { return nil }
func (l *fakeLease) CompleteCurrent(context.Context) error                          { return nil }
func (l *fakeLease) Defer(context.Context, time.Time, string, string, string) error { return nil }
func (l *fakeLease) Release(_ context.Context, reason ReleaseReason) error {
	l.lastRelease = reason
	l.releaseCalls.Add(1)
	return nil
}

func TestRenewFailureCancelsFetchAndRunJoins(t *testing.T) {
	config := testConfig()
	config.RenewInterval = 10 * time.Millisecond
	repository := &Repository{config: config}
	lease := &fakeLease{}
	joined := make(chan struct{})
	result := repository.Run(context.Background(), lease, func(ctx context.Context, _ contract.LeaseProof) error {
		defer close(joined)
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(result.Err, ErrFenceLost) || result.Outcome != LeaseRunFenceLost {
		t.Fatalf("run result = %#v", result)
	}
	select {
	case <-joined:
	default:
		t.Fatal("fetch goroutine was not joined")
	}
	if lease.renewCalls.Load() != 1 || lease.releaseCalls.Load() != 0 {
		t.Fatalf("renew/release calls = %d/%d", lease.renewCalls.Load(), lease.releaseCalls.Load())
	}
}

func TestFenceLossPrefersBufferedCallbackResult(t *testing.T) {
	callbackErr := errors.New("callback failed")
	for _, testCase := range []struct {
		name    string
		err     error
		outcome LeaseRunOutcome
	}{
		{name: "completed", outcome: LeaseRunCallbackCompleted},
		{name: "failed", err: callbackErr, outcome: LeaseRunCallbackFailed},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &Repository{config: testConfig()}
			lease := &fakeLease{}
			runCtx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			result <- testCase.err
			got := repository.finishRenewFailure(runCtx, cancel, lease, result, ErrFenceLost)
			if got.Outcome != testCase.outcome || !errors.Is(got.Err, testCase.err) {
				t.Fatalf("run result = %#v, want outcome %s and error %v", got, testCase.outcome, testCase.err)
			}
			if lease.releaseCalls.Load() != 0 {
				t.Fatalf("release calls = %d, want 0", lease.releaseCalls.Load())
			}
		})
	}
}

func TestRunReturnsRunnerPanic(t *testing.T) {
	repository := &Repository{config: testConfig()}
	result := repository.Run(context.Background(), &fakeLease{}, func(context.Context, contract.LeaseProof) error {
		panic("runner panic")
	})
	if result.Err == nil || !strings.Contains(result.Err.Error(), "collection-job-run: recovered panic: runner panic") {
		t.Fatalf("Run() result = %#v, want recovered runner panic", result)
	}
}

func TestRenewFailureBoundsNonCooperativeRunnerJoin(t *testing.T) {
	config := testConfig()
	config.RenewInterval = 5 * time.Millisecond
	config.CleanupTimeout = 20 * time.Millisecond
	config.DBTimeout = 10 * time.Millisecond
	repository := &Repository{config: config}
	lease := &fakeLease{renewErr: errors.New("renew database unavailable")}
	releaseRunner := make(chan struct{})
	started := time.Now()
	result := repository.Run(context.Background(), lease, func(context.Context, contract.LeaseProof) error {
		<-releaseRunner
		return nil
	})
	close(releaseRunner)
	if !errors.Is(result.Err, context.DeadlineExceeded) || result.Outcome != LeaseRunCleanupTimedOut {
		t.Fatalf("run result = %#v, want renew failure and bounded join timeout", result)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("Run() elapsed = %s, want bounded cleanup", elapsed)
	}
	if lease.releaseCalls.Load() != 1 || lease.lastRelease != ReleaseRenewFail {
		t.Fatalf("release calls = %d reason = %q, want renew-failure release", lease.releaseCalls.Load(), lease.lastRelease)
	}
}

func TestCancellationReleasesBeforeBoundedRunnerJoin(t *testing.T) {
	config := testConfig()
	config.RenewInterval = time.Second
	config.CleanupTimeout = 20 * time.Millisecond
	config.DBTimeout = 10 * time.Millisecond
	repository := &Repository{config: config}
	lease := &fakeLease{}
	releaseRunner := make(chan struct{})
	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	result := repository.Run(runCtx, lease, func(context.Context, contract.LeaseProof) error {
		<-releaseRunner
		return nil
	})
	close(releaseRunner)
	if !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, context.DeadlineExceeded) ||
		result.Outcome != LeaseRunCleanupTimedOut {
		t.Fatalf("run result = %#v, want cancellation and bounded join timeout", result)
	}
	if lease.releaseCalls.Load() != 1 || lease.lastRelease != ReleaseShutdown {
		t.Fatalf("release calls = %d reason = %q, want 1 shutdown", lease.releaseCalls.Load(), lease.lastRelease)
	}
}

type leaseTarget struct {
	subject  string
	kind     contract.ObservationKind
	interval time.Duration
	enabled  bool
}

func seedProjection(t *testing.T, pool *pgxpool.Pool, targets []leaseTarget) int64 {
	t.Helper()
	ctx := context.Background()
	var generation int64
	if err := pool.QueryRow(ctx, mustTestSQL("insert_projection.sql"), len(targets)).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if _, err := pool.Exec(ctx, mustTestSQL("insert_target.sql"), generation, target.subject, target.kind, target.interval.Milliseconds(), target.enabled); err != nil {
			t.Fatal(err)
		}
	}
	return generation
}

func newTestRepository(t *testing.T, pool *pgxpool.Pool) *Repository {
	t.Helper()
	config := testConfig()
	repository, err := NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func communityJob() *JobSpec {
	const subject = "channel:a"
	return &JobSpec{
		JobKey:   "collector:youtubejs:community_collect:" + subject,
		Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "community_collect", SubjectKey: subject, PollInterval: time.Minute,
	}
}

func candidateJobs(t *testing.T, repository *Repository, provider contract.Provider, kind string, limit int) []JobSpec {
	t.Helper()
	return candidatePage(t, repository, provider, kind, nil, limit).Jobs
}

func candidatePage(t *testing.T, repository *Repository, provider contract.Provider, kind string, excluded []string, limit int) CandidatePage {
	t.Helper()
	ctx := context.Background()
	generation, err := repository.CurrentProjectionGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	job, ok := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: provider, Kind: sourceobservation.JobKind(kind),
	})
	if !ok {
		t.Fatalf("missing job contract %s/%s", provider, kind)
	}
	page, err := repository.CandidatesForProjection(ctx, generation, job, excluded, limit)
	if err != nil {
		t.Fatal(err)
	}
	return page
}
