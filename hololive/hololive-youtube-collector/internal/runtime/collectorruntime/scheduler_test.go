package collectorruntime

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
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
	cfg.YouTubeJSRequestTimeout = 20 * time.Second
	lease, err := leaseConfigFrom(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if lease.WorkerCount != 3 || lease.QueueCapacity != 12 || lease.RenewTimeout != cfg.RenewTimeout {
		t.Fatalf("lease config = %+v", lease)
	}
}

func TestLeaseSchedulerDefersFailedCollect(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedRuntimeCommunityTarget(t, pool, "UC_TEST")
	config := joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		RenewTimeout: 50 * time.Millisecond, DBTimeout: 250 * time.Millisecond, CleanupTimeout: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}
	failing := stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage)
	failing.collect = func(context.Context, *collectutil.RunInput) (collectutil.CollectResult, error) {
		return collectutil.CollectResult{}, errors.New("provider unavailable")
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
		gates:     defaultProviderGates(),
		queued:    make(map[string]struct{}), queue: make(chan joblease.JobSpec, config.QueueCapacity),
	}
	spec := joblease.JobSpec{
		JobKey:   "collector:youtubejs:community_collect:UC_TEST",
		Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "community_collect", SubjectKey: "UC_TEST", PollInterval: time.Minute,
	}
	scheduler.runSpec(ctx, &spec)
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
		RenewTimeout: 50 * time.Millisecond, DBTimeout: 250 * time.Millisecond, CleanupTimeout: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}
	retryAt := time.Now().UTC().Add(200 * time.Millisecond)
	failing := stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage)
	failing.collect = func(context.Context, *collectutil.RunInput) (collectutil.CollectResult, error) {
		return collectutil.CollectResult{}, collecterr.CooldownUntil("limited", retryAt)
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
		gates:     defaultProviderGates(),
		queued:    make(map[string]struct{}), queue: make(chan joblease.JobSpec, config.QueueCapacity),
	}
	spec := joblease.JobSpec{
		JobKey:   "collector:youtubejs:community_collect:UC_TEST",
		Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "community_collect", SubjectKey: "UC_TEST", PollInterval: time.Minute,
	}
	scheduler.runSpec(ctx, &spec)
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
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		chunk, err := fs.ReadFile(os.DirFS("."), path)
		if err != nil {
			t.Fatal(err)
		}
		source.Write(chunk)
		source.WriteByte('\n')
	}
	body := source.String()
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
		{"vid-1", contract.KindViewerSample},
	})
	config := joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		RenewTimeout: 50 * time.Millisecond, DBTimeout: 250 * time.Millisecond, CleanupTimeout: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}
	holodex := stubJob(contract.ProviderHolodex, "holodex_live",
		contract.KindLiveSnapshot, contract.KindViewerSample)
	holodex.collect = func(_ context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
		lease := input.Lease()
		live, err := collectutil.Envelope(
			contract.ProviderHolodex, contract.KindLiveSnapshot, "UC_TEST", 1, &lease,
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
			return collectutil.CollectResult{}, err
		}
		viewerCount := int64(9)
		viewer, err := collectutil.Envelope(
			contract.ProviderHolodex, contract.KindViewerSample, "vid-1", 1, &lease,
			contract.CompletenessComplete, contract.ContinuityNotApplicable,
			contract.ViewerSampleV1{
				VideoID: "vid-1", ViewerCount: &viewerCount, Availability: "AVAILABLE",
				SampleWindowStart: lease.ScheduledFor, SampleWindowSeconds: 60,
				Coverage: contract.ViewerSampleCoverageV1{
					VideoID: "vid-1", SampleWindowStart: lease.ScheduledFor, SampleWindowSeconds: 60,
				},
			},
		)
		if err != nil {
			return collectutil.CollectResult{}, err
		}
		return collectutil.CompleteFromEnvelopes([]contract.Envelope{live, viewer}, time.Now())
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
		gates:     defaultProviderGates(),
		queued:    make(map[string]struct{}), queue: make(chan joblease.JobSpec, config.QueueCapacity),
	}
	spec := joblease.JobSpec{
		JobKey: "collector:holodex:holodex_live:global", Provider: contract.ProviderHolodex, Class: "GLOBAL",
		CollectionJobKind: "holodex_live", SubjectKey: "global:holodex_live", PollInterval: time.Minute,
	}
	scheduler.runSpec(ctx, &spec)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_observations WHERE provider = 'holodex'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("observations = %d, want 2", count)
	}
}

func TestLeaseSchedulerPublishesPartialAndDefersAtomically(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{"UC_TEST", contract.KindVideoList},
		{"UC_TEST", contract.KindShortsList},
	})
	config := joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		RenewTimeout: 50 * time.Millisecond, DBTimeout: 250 * time.Millisecond, CleanupTimeout: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}
	content := stubJob(contract.ProviderYouTubeJS, "youtubejs_content", contract.KindVideoList, contract.KindShortsList)
	content.collect = func(_ context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
		lease := input.Lease()
		envelope, buildErr := collectutil.Envelope(
			contract.ProviderYouTubeJS, contract.KindVideoList, input.Spec().SubjectKey, 1, &lease,
			contract.CompletenessComplete, contract.ContinuityContiguous,
			contract.VideoListV1{
				ChannelID: input.Spec().SubjectKey,
				Videos:    []contract.VideoListItemV1{},
				Coverage: contract.ChannelListCoverageV1{
					ChannelID: input.Spec().SubjectKey, MaxResults: 10, Exhausted: true,
				},
			},
		)
		if buildErr != nil {
			return collectutil.CollectResult{}, buildErr
		}
		output, buildErr := collectutil.OutputFromEnvelopes([]contract.Envelope{envelope}, time.Now())
		if buildErr != nil {
			return collectutil.CollectResult{}, buildErr
		}
		return collectutil.NewPartialResult(
			output,
			collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "shorts timeout"),
			contract.KindShortsList,
		)
	}
	registry, err := NewRegistry(withOverride(content)...)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := &leaseScheduler{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   "collector-a", logger: slog.New(slog.NewTextHandler(io.Discard, nil)), config: config,
		collector: settings.DefaultYouTubeCollectorConfig(),
		gates:     defaultProviderGates(), queued: make(map[string]struct{}),
		queue: make(chan joblease.JobSpec, config.QueueCapacity), fatal: make(chan error, 1),
	}
	spec := joblease.JobSpec{
		JobKey: "collector:youtubejs:youtubejs_content:UC_TEST", Provider: contract.ProviderYouTubeJS, Class: "SUBJECT",
		CollectionJobKind: "youtubejs_content", SubjectKey: "UC_TEST", PollInterval: time.Minute,
	}
	scheduler.runSpec(ctx, &spec)
	var count int
	var state string
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM source_observations WHERE provider = 'youtubejs' AND observation_kind = 'video_list'
	`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT slot_state FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if count != 1 || state != "DEFERRED" {
		t.Fatalf("partial terminal state count=%d state=%s", count, state)
	}
}

func TestLeaseSchedulerStopJoinsWorkers(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedRuntimeCommunityTarget(t, pool, "UC_TEST")
	config := joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		RenewTimeout: 50 * time.Millisecond, DBTimeout: 250 * time.Millisecond, CleanupTimeout: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, WorkerCount: 1, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(completeStubRunners()...)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := &leaseScheduler{
		repository: repository, candidates: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   "collector-a", logger: slog.New(slog.NewTextHandler(io.Discard, nil)), config: config,
		collector: settings.DefaultYouTubeCollectorConfig(),
		gates:     defaultProviderGates(),
		state:     SchedulerNew, queued: make(map[string]struct{}),
		queue: make(chan joblease.JobSpec, config.QueueCapacity), fatal: make(chan error, 1),
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if scheduler.Snapshot().State != SchedulerStopped {
		t.Fatalf("state = %s, want STOPPED", scheduler.Snapshot().State)
	}
}

func TestLeaseSchedulerStopTimeoutKeepsRunStateUntilJoin(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	done := make(chan struct{})
	_, cancel := context.WithCancel(t.Context())
	scheduler := &leaseScheduler{
		repository: new(joblease.Repository),
		registry:   new(Registry),
		config:     joblease.Config{WorkerCount: 1, QueueCapacity: 1},
		state:      SchedulerRunning,
		cancel:     cancel,
		done:       done,
		queued:     make(map[string]struct{}),
		fatal:      make(chan error, 1),
	}
	scheduler.wg.Go(func() {
		<-release
	})
	go scheduler.join(done)

	stopCtx, stopCancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stopCancel()
	if err := scheduler.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want deadline exceeded", err)
	}
	if err := scheduler.Start(t.Context()); err == nil {
		t.Fatal("Start during STOPPING was accepted")
	}
	if scheduler.Snapshot().State != SchedulerStopping {
		t.Fatalf("state = %s, want STOPPING", scheduler.Snapshot().State)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler join did not finish after worker release")
	}
	if scheduler.Snapshot().State != SchedulerStopped {
		t.Fatal("scheduler was not STOPPED after join")
	}
	if err := scheduler.Start(t.Context()); err == nil {
		t.Fatal("STOPPED scheduler was reused")
	}
}

func withOverride(override JobRunner) []JobRunner {
	runners := completeStubRunners()
	for i, runner := range runners {
		if runner.JobID() == override.JobID() {
			runners[i] = override
		}
	}
	return runners
}

func defaultProviderGates() map[contract.Provider]chan struct{} {
	cfg := settings.DefaultYouTubeCollectorConfig()
	return newProviderGates(&cfg)
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
