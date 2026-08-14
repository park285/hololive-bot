package collectorruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/prometheus/client_golang/prometheus"
)

func TestLeaseConfigFromUsesCollectorBudgets(t *testing.T) {
	t.Parallel()
	cfg := settings.DefaultYouTubeCollectorConfig()
	cfg.TotalWorkers = 3
	cfg.QueueCapacity = 12
	cfg.AcquisitionBatch = 12
	cfg.YouTubeJSTimeout = 20 * time.Second
	lease, err := leaseConfigFrom(cfg, 15*time.Second, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if lease.WorkerCount != 3 || lease.QueueCapacity != 12 || lease.ProviderTimeout != 20*time.Second {
		t.Fatalf("lease config = %+v", lease)
	}
}

func TestLeaseSchedulerDefersFailedCollect(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedRuntimeCommunityTarget(t, pool, "UC_TEST")
	config := joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		ProviderTimeout: 500 * time.Millisecond, NormalizationBudget: 250 * time.Millisecond, PublishBudget: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, config)
	if err != nil {
		t.Fatal(err)
	}
	failing := stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage)
	failing.collect = func(context.Context, collectutil.RunInput) (collectutil.RunOutput, error) {
		return collectutil.RunOutput{}, errors.New("provider unavailable")
	}
	registry, err := NewRegistry(withOverride(failing)...)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := &leaseScheduler{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   "collector-a", logger: slog.New(slog.NewTextHandler(io.Discard, nil)), config: config,
		collector: settings.DefaultYouTubeCollectorConfig(),
		gates:     newProviderGates(settings.DefaultYouTubeCollectorConfig()),
		queued:    make(map[string]struct{}), queue: make(chan joblease.JobSpec, config.QueueCapacity),
	}
	spec := joblease.JobSpec{
		JobKey:   "collector:youtubejs:community_collect:UC_TEST",
		Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "community_collect", SubjectKey: "UC_TEST", PollInterval: time.Minute,
	}
	scheduler.runSpec(ctx, spec)
	var state string
	if err := pool.QueryRow(ctx, `
		SELECT slot_state FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "DEFERRED" {
		t.Fatalf("state = %s", state)
	}
}

func TestLeaseSchedulerDefersCooldownUntilRetryAt(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedRuntimeCommunityTarget(t, pool, "UC_TEST")
	config := joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		ProviderTimeout: 500 * time.Millisecond, NormalizationBudget: 250 * time.Millisecond, PublishBudget: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, config)
	if err != nil {
		t.Fatal(err)
	}
	retryAt := time.Now().UTC().Add(200 * time.Millisecond)
	failing := stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage)
	failing.collect = func(context.Context, collectutil.RunInput) (collectutil.RunOutput, error) {
		return collectutil.RunOutput{}, collecterr.CooldownUntil("limited", retryAt)
	}
	registry, err := NewRegistry(withOverride(failing)...)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := &leaseScheduler{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   "collector-a", logger: slog.New(slog.NewTextHandler(io.Discard, nil)), config: config,
		collector: settings.DefaultYouTubeCollectorConfig(),
		gates:     newProviderGates(settings.DefaultYouTubeCollectorConfig()),
		queued:    make(map[string]struct{}), queue: make(chan joblease.JobSpec, config.QueueCapacity),
	}
	spec := joblease.JobSpec{
		JobKey:   "collector:youtubejs:community_collect:UC_TEST",
		Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "community_collect", SubjectKey: "UC_TEST", PollInterval: time.Minute,
	}
	scheduler.runSpec(ctx, spec)
	var deferred time.Time
	if err := pool.QueryRow(ctx, `
		SELECT retry_not_before FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey).Scan(&deferred); err != nil {
		t.Fatal(err)
	}
	if deferred.Before(retryAt.Add(-50*time.Millisecond)) || deferred.After(retryAt.Add(50*time.Millisecond)) {
		t.Fatalf("retry_not_before = %s, want %s", deferred, retryAt)
	}
}

func TestProductionSchedulerHasNoUnleasedPollPath(t *testing.T) {
	source, err := os.ReadFile("scheduler.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if strings.Contains(body, "PollWithLease") || strings.Contains(body, "communitycollector") ||
		strings.Contains(body, "currentCommunityContractGeneration") {
		t.Fatal("production scheduler must not keep the Community-only publish path")
	}
	if !strings.Contains(body, "Collect(") || !strings.Contains(body, "Publish(") || !strings.Contains(body, "internal/runtime/joblease") {
		t.Fatal("production scheduler must collect through the typed registry and PublishBatch")
	}
	if !strings.Contains(body, "ObserveFreshness") {
		t.Fatal("production scheduler must refresh collection freshness")
	}
}

func TestLeaseSchedulerPublishesOneBatchForMultipleKinds(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{"UC_TEST", contract.KindLiveSnapshot},
		{"UC_TEST", contract.KindChannelStats},
	})
	config := joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		ProviderTimeout: 500 * time.Millisecond, NormalizationBudget: 250 * time.Millisecond, PublishBudget: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, config)
	if err != nil {
		t.Fatal(err)
	}
	holodex := stubJob(contract.ProviderHolodex, "holodex_global",
		contract.KindLiveSnapshot, contract.KindViewerSample, contract.KindChannelStats,
		contract.KindChannelProfile, contract.KindChannelPhoto, contract.KindSchedule)
	holodex.collect = func(_ context.Context, input collectutil.RunInput) (collectutil.RunOutput, error) {
		live, err := collectutil.Envelope(
			contract.ProviderHolodex, contract.KindLiveSnapshot, "UC_TEST", 1, input.Lease,
			contract.CompletenessPartial, contract.ContinuityNotApplicable,
			contract.LiveSnapshotV1{
				Sessions: []contract.LiveSessionV1{{VideoID: "vid-1", ChannelID: "UC_TEST", Status: "LIVE"}},
				Coverage: contract.GlobalChannelCoverageV1{
					RequestedChannelIDs: []string{"UC_TEST"},
					Filters:             contract.LiveFiltersV1{Statuses: []string{"LIVE"}},
				},
			},
		)
		if err != nil {
			return collectutil.RunOutput{}, err
		}
		stats, err := collectutil.Envelope(
			contract.ProviderHolodex, contract.KindChannelStats, "UC_TEST", 1, input.Lease,
			contract.CompletenessPartial, contract.ContinuityNotApplicable,
			contract.ChannelStatsV1{
				ChannelID: "UC_TEST", SubscriberCount: int64Ptr(9),
				Coverage: contract.ChannelStatsCoverageV1{ChannelID: "UC_TEST", Fields: []string{"subscriber_count"}},
			},
		)
		if err != nil {
			return collectutil.RunOutput{}, err
		}
		return collectutil.Output([]contract.Envelope{live, stats}, time.Now())
	}
	registry, err := NewRegistry(withOverride(holodex)...)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := &leaseScheduler{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   "collector-a", logger: slog.New(slog.NewTextHandler(io.Discard, nil)), config: config,
		collector: settings.DefaultYouTubeCollectorConfig(),
		gates:     newProviderGates(settings.DefaultYouTubeCollectorConfig()),
		queued:    make(map[string]struct{}), queue: make(chan joblease.JobSpec, config.QueueCapacity),
	}
	spec := joblease.JobSpec{
		JobKey: "collector:holodex:global", Provider: contract.ProviderHolodex, Class: "GLOBAL",
		CollectionJobKind: "holodex_global", SubjectKey: "global:holodex_global", PollInterval: time.Minute,
	}
	scheduler.runSpec(ctx, spec)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_observations WHERE provider = 'holodex'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("observations = %d, want 2", count)
	}
}

func TestLeaseSchedulerStopJoinsWorkers(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedRuntimeCommunityTarget(t, pool, "UC_TEST")
	config := joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		ProviderTimeout: 500 * time.Millisecond, NormalizationBudget: 250 * time.Millisecond, PublishBudget: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, config)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(completeStubRunners()...)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := &leaseScheduler{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   "collector-a", logger: slog.New(slog.NewTextHandler(io.Discard, nil)), config: config,
		collector: settings.DefaultYouTubeCollectorConfig(),
		gates:     newProviderGates(settings.DefaultYouTubeCollectorConfig()),
		queued:    make(map[string]struct{}), queue: make(chan joblease.JobSpec, config.QueueCapacity),
	}
	scheduler.Start(context.Background())
	scheduler.Stop()
}

func withOverride(override JobRunner) []JobRunner {
	runners := completeStubRunners()
	for i, runner := range runners {
		if runner.Provider() == override.Provider() && runner.JobKind() == override.JobKind() {
			runners[i] = override
		}
	}
	return runners
}

type leaseSeed struct {
	subject string
	kind    contract.ObservationKind
}

func seedRuntimeCommunityTarget(t *testing.T, pool *pgxpool.Pool, subject string) {
	t.Helper()
	seedRuntimeTargets(t, pool, []leaseSeed{{subject, contract.KindCommunityPage}})
}

func seedRuntimeTargets(t *testing.T, pool *pgxpool.Pool, targets []leaseSeed) {
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
			) VALUES ($1, $2, $3, 50, 60000, TRUE, clock_timestamp() + INTERVAL '1 hour')
		`, generation, target.subject, target.kind); err != nil {
			t.Fatal(err)
		}
	}
}

func int64Ptr(value int64) *int64 { return &value }
