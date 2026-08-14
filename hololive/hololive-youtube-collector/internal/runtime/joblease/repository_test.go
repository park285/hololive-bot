package joblease

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func testConfig() Config {
	return Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		ProviderTimeout: 500 * time.Millisecond, NormalizationBudget: 250 * time.Millisecond, PublishBudget: 250 * time.Millisecond,
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
	config.LeaseTTL = config.ProviderTimeout + config.NormalizationBudget + config.PublishBudget
	if !errors.Is(config.Validate(), ErrInvalidConfig) {
		t.Fatalf("lease budget validation error = %v", config.Validate())
	}
}

func TestAcquireIncrementsEpochAndTakeoverPreservesScheduledSlot(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob("channel:a", time.Minute)

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
	if _, err := pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE job_key = $1`, spec.JobKey); err != nil {
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

func TestOnlyOneGlobalHolderIsActive(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"global:hololive-schedule", contract.KindSchedule, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := JobSpec{
		JobKey: "collector:hololive_official:global", Provider: contract.ProviderHololiveOfficial,
		Class: "GLOBAL", CollectionJobKind: "official_schedule",
		SubjectKey: "global:hololive-schedule", PollInterval: time.Minute,
	}
	if _, err := repository.Acquire(ctx, spec, "collector-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Acquire(ctx, spec, "collector-b"); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("second global acquire error = %v", err)
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM youtube_collection_job_leases WHERE job_key = $1 AND slot_state = 'ACTIVE'`, spec.JobKey).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active global holders = %d", active)
	}
}

func TestHolodexGlobalCandidatesUseFastestIntervalWhenKindsDiffer(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{"UC_A", contract.KindLiveSnapshot, 2 * time.Minute, true},
		{"UC_A", contract.KindChannelStats, 6 * time.Hour, true},
	})
	repository := newTestRepository(t, pool)
	candidates, err := repository.Candidates(ctx, contract.ProviderHolodex, "holodex_global", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].PollInterval != 2*time.Minute {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestIdleAcquisitionCoalescesLongOutage(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	spec := communityJob("channel:a", time.Minute)
	lease, err := repository.Acquire(ctx, spec, "collector-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases
		SET next_due_at = clock_timestamp() - INTERVAL '10 minutes'
		WHERE job_key = $1
	`, spec.JobKey); err != nil {
		t.Fatal(err)
	}
	coalesced, err := repository.Acquire(ctx, spec, "collector-b")
	if err != nil {
		t.Fatal(err)
	}
	var recent bool
	if err := pool.QueryRow(ctx, `SELECT $1 >= clock_timestamp() - INTERVAL '1 minute' AND $1 <= clock_timestamp()`, coalesced.Proof().ScheduledFor).Scan(&recent); err != nil {
		t.Fatal(err)
	}
	if !recent {
		t.Fatalf("scheduled slot was not coalesced: %s", coalesced.Proof().ScheduledFor)
	}
}

func TestDeferAndReleasePreserveScheduledSlot(t *testing.T) {
	for _, action := range []string{"defer", "release"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			pool := dbtest.NewPool(t)
			seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
			repository := newTestRepository(t, pool)
			spec := communityJob("channel:a", time.Minute)
			first, err := repository.Acquire(ctx, spec, "collector-a")
			if err != nil {
				t.Fatal(err)
			}
			if action == "defer" {
				err = first.Defer(ctx, time.Now().UTC().Add(500*time.Millisecond), "provider_timeout")
			} else {
				err = first.Release(ctx)
			}
			if err != nil {
				t.Fatalf("%s: %v", action, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET retry_not_before = clock_timestamp() - INTERVAL '1 millisecond' WHERE job_key = $1`, spec.JobKey); err != nil {
				t.Fatal(err)
			}
			second, err := repository.Acquire(ctx, spec, "collector-b")
			if err != nil {
				t.Fatalf("reacquire after %s: %v", action, err)
			}
			if !second.Proof().ScheduledFor.Equal(first.Proof().ScheduledFor) || second.Proof().FenceEpoch != first.Proof().FenceEpoch+1 {
				t.Fatalf("slot changed after %s: first=%#v second=%#v", action, first.Proof(), second.Proof())
			}
		})
	}
}

func TestProjectionExpiryBlocksAcquisition(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	if _, err := pool.Exec(ctx, `UPDATE youtube_collection_projection_generations SET valid_until = clock_timestamp() - INTERVAL '1 second' WHERE generation = $1`, generation); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestRepository(t, pool).Acquire(ctx, communityJob("channel:a", time.Minute), "collector-a"); !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("expired projection acquire error = %v", err)
	}
}

func TestYouTubeSubjectJobsDistributeWithoutDuplicateAcquisition(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{"channel:a", contract.KindCommunityPage, time.Minute, true},
		{"channel:b", contract.KindCommunityPage, time.Minute, true},
	})
	repository := newTestRepository(t, pool)
	candidates, err := repository.Candidates(ctx, contract.ProviderYouTubeJS, "community_collect", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d", len(candidates))
	}
	first, err := repository.Acquire(ctx, candidates[0], "collector-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Acquire(ctx, candidates[0], "collector-b"); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("duplicate acquisition error = %v", err)
	}
	second, err := repository.Acquire(ctx, candidates[1], "collector-b")
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
}

func (l *fakeLease) Proof() contract.LeaseProof { return l.proof }
func (l *fakeLease) Renew(context.Context) error {
	l.renewCalls.Add(1)
	return ErrFenceLost
}
func (l *fakeLease) Complete(context.Context) error                 { return nil }
func (l *fakeLease) Defer(context.Context, time.Time, string) error { return nil }
func (l *fakeLease) Release(context.Context) error {
	l.releaseCalls.Add(1)
	return nil
}

func TestRenewFailureCancelsFetchAndRunJoins(t *testing.T) {
	config := testConfig()
	config.RenewInterval = 10 * time.Millisecond
	repository := &Repository{config: config}
	lease := &fakeLease{}
	joined := make(chan struct{})
	err := repository.Run(context.Background(), lease, func(ctx context.Context, _ contract.LeaseProof) error {
		defer close(joined)
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, ErrFenceLost) {
		t.Fatalf("run error = %v", err)
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
	if err := pool.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until, activated_at
		) VALUES ('CURRENT', $1, repeat('a', 64), clock_timestamp() + INTERVAL '1 hour', clock_timestamp())
		RETURNING generation
	`, len(targets)).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if _, err := pool.Exec(ctx, `
			INSERT INTO youtube_collection_targets (
				projection_generation, subject_key, observation_kind,
				priority, poll_interval_ms, enabled, valid_until
			) VALUES ($1, $2, $3, 50, $4, $5, clock_timestamp() + INTERVAL '1 hour')
		`, generation, target.subject, target.kind, target.interval.Milliseconds(), target.enabled); err != nil {
			t.Fatal(err)
		}
	}
	return generation
}

func newTestRepository(t *testing.T, pool *pgxpool.Pool) *Repository {
	t.Helper()
	repository, err := NewRepository(pool, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func communityJob(subject string, interval time.Duration) JobSpec {
	return JobSpec{
		JobKey:   "collector:youtubejs:community_collect:" + subject,
		Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "community_collect", SubjectKey: subject, PollInterval: interval,
	}
}
