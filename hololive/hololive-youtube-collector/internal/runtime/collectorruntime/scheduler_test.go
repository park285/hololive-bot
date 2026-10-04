package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TestLeaseConfigFromUsesCollectorBudgets(t *testing.T) {
	t.Parallel()

	cfg := collectorconfig.DefaultConfig()

	cfg.TotalWorkers = 3
	cfg.QueueCapacity = 12
	cfg.AcquisitionBatch = 12
	cfg.YouTubeJSRequestTimeout = 20 * time.Second

	lease, err := leaseConfigFrom(&cfg)
	if err != nil {
		t.Fatal(err)
	}

	if lease.AcquisitionBatch != 12 || lease.QueueCapacity != 12 || lease.RenewTimeout != cfg.RenewTimeout {
		t.Fatalf("lease config = %+v", lease)
	}

	scheduler := newScheduler(&collectionExecutor{}, nil, &cfg)
	if scheduler.workers != 3 || scheduler.queueCapacity != 12 || cap(scheduler.queue) != 12 ||
		scheduler.pollCadence != cfg.AcquisitionCadence || scheduler.queueMaxAge != cfg.QueueMaxAge {
		t.Fatalf("scheduler policy = workers %d queue %d/%d cadence %s max age %s",
			scheduler.workers, scheduler.queueCapacity, cap(scheduler.queue), scheduler.pollCadence, scheduler.queueMaxAge)
	}
}

func TestLeaseSchedulerDefersFailedCollect(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeCommunityTarget(t, pool)

	config := runtimeLeaseConfig()

	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	failing := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

	failing.collect = func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
		return collection.CollectResult{}, errors.New("provider unavailable")
	}

	registry, err := newTestRegistry(withOverride(failing)...)
	if err != nil {
		t.Fatal(err)
	}

	executor := &collectionExecutor{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   testOwnerInstance, logger: slog.New(slog.DiscardHandler), retryBounds: testRetryBounds,
		collector: collectorconfig.DefaultConfig(),
		gates:     defaultProviderGates(),
	}
	spec := joblease.JobSpec{
		JobKey:   "collector:youtubejs:community_collect:UC_TEST",
		Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: testCommunityJobKind, SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}
	executor.runSpec(ctx, &spec)

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
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeCommunityTarget(t, pool)

	config := runtimeLeaseConfig()

	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	retryAt := time.Now().UTC().Add(200 * time.Millisecond)
	failing := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

	failing.collect = func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
		return collection.CollectResult{}, collecterr.CooldownUntil("limited", retryAt)
	}

	registry, err := newTestRegistry(withOverride(failing)...)
	if err != nil {
		t.Fatal(err)
	}

	executor := &collectionExecutor{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   testOwnerInstance, logger: slog.New(slog.DiscardHandler), retryBounds: testRetryBounds,
		collector: collectorconfig.DefaultConfig(),
		gates:     defaultProviderGates(),
	}
	spec := joblease.JobSpec{
		JobKey:   "collector:youtubejs:community_collect:UC_TEST",
		Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: testCommunityJobKind, SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}
	executor.runSpec(ctx, &spec)

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

func TestLeaseSchedulerPublishesOneBatchForMultipleKinds(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindChannelProfile},
		{testSubjectKey, contract.KindChannelPhoto},
	})

	// 이 테스트는 짧은 갱신 제한 시간이 아니라 두 종류의 원자적 발행을 검증합니다.
	collector := collectorconfig.DefaultConfig()

	config, err := leaseConfigFrom(&collector)
	if err != nil {
		t.Fatal(err)
	}

	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	youtubejs := stubJob(contract.ProviderYouTubeJS, "youtubejs_channel_metadata",
		contract.KindChannelProfile, contract.KindChannelPhoto)

	youtubejs.collect = collectYouTubeJSMetadata

	registry, err := newTestRegistry(withOverride(youtubejs)...)
	if err != nil {
		t.Fatal(err)
	}

	executor := &collectionExecutor{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   testOwnerInstance, logger: slog.New(slog.DiscardHandler), retryBounds: testRetryBounds,
		collector: collector,
		gates:     defaultProviderGates(),
	}
	spec := joblease.JobSpec{
		JobKey: "collector:youtubejs:youtubejs_channel_metadata:UC_TEST", Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: "youtubejs_channel_metadata", SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}
	executor.runSpec(ctx, &spec)

	var count int

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM source_observations WHERE provider = 'youtubejs'`).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatalf("observations = %d, want 2", count)
	}
}

func TestLeaseSchedulerPublishesPartialAndDefersAtomically(t *testing.T) {
	recorder := newCollectionTraceRecorder(t)
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindVideoList},
		{testSubjectKey, contract.KindShortsList},
	})

	config := runtimeLeaseConfig()

	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	content := stubJob(contract.ProviderYouTubeJS, "youtubejs_content", contract.KindVideoList, contract.KindShortsList)

	content.collect = collectYouTubeJSPartialVideoList

	registry, err := newTestRegistry(withOverride(content)...)
	if err != nil {
		t.Fatal(err)
	}

	executor := &collectionExecutor{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   testOwnerInstance, logger: slog.New(slog.DiscardHandler), retryBounds: testRetryBounds,
		collector: collectorconfig.DefaultConfig(),
		gates:     defaultProviderGates(),
	}
	spec := joblease.JobSpec{
		JobKey: "collector:youtubejs:youtubejs_content:UC_TEST", Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: "youtubejs_content", SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}
	executor.runSpec(ctx, &spec)
	assertPartialCollectionSpans(t, recorder.Ended())

	var (
		count int
		state string
	)

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
	seedRuntimeCommunityTarget(t, pool)

	config := runtimeLeaseConfig()

	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	registry, err := newTestRegistry(completeStubRunners()...)
	if err != nil {
		t.Fatal(err)
	}

	collector := runtimeCollectorConfig()
	scheduler := newScheduler(&collectionExecutor{
		repository: repository, registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   testOwnerInstance, logger: slog.New(slog.DiscardHandler), retryBounds: testRetryBounds,
		collector: collector,
		gates:     defaultProviderGates(),
	}, repository, &collector)

	if err := scheduler.Start(t.Context()); err != nil {
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
		executor: &collectionExecutor{
			repository: new(joblease.Repository),
			registry:   new(Registry),
		},
		workers: 1, queueCapacity: 1,
		state:  SchedulerRunning,
		cancel: cancel,
		done:   done,
		queued: make(map[string]struct{}),
		fatal:  make(chan error, 1),
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

const (
	testJobKey           = "job"
	testSubjectKey       = "UC_TEST"
	testCommunityJobKind = "community_collect"
	testOwnerInstance    = "collector-a"
)

func collectYouTubeJSMetadata(_ context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	lease := input.Lease()
	subject := input.Subject()

	profile, err := collection.Envelope(
		contract.ProviderYouTubeJS, contract.KindChannelProfile, subject, 1, &lease,
		contract.CompletenessPartial, contract.ContinuityNotApplicable,
		contract.ChannelProfileV1{
			ChannelID: subject,
			Handle:    contract.FieldValue[string]{Present: true, Value: "@test"},
			Coverage: contract.ChannelProfileCoverageV1{
				ChannelID: subject, Fields: []string{"handle"},
			},
		},
	)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("envelope: %w", err)
	}

	photo, err := collection.Envelope(
		contract.ProviderYouTubeJS, contract.KindChannelPhoto, subject, 1, &lease,
		contract.CompletenessPartial, contract.ContinuityNotApplicable,
		contract.ChannelPhotoV1{
			ChannelID: subject,
			Variants:  []contract.PhotoVariantV1{{Kind: "avatar", URL: "https://img.test/avatar.jpg"}},
			Coverage: contract.ChannelPhotoCoverageV1{
				ChannelID: subject, Variants: []string{"avatar"},
			},
		},
	)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("envelope: %w", err)
	}

	complete, err := collection.CompleteFromEnvelopes([]contract.Envelope{profile, photo}, time.Now())
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("complete from envelopes: %w", err)
	}

	return complete, nil
}

func collectYouTubeJSPartialVideoList(_ context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	lease := input.Lease()

	envelope, err := collection.Envelope(
		contract.ProviderYouTubeJS, contract.KindVideoList, input.Subject(), contract.VideoListPublicationContractGeneration, &lease,
		contract.CompletenessComplete, contract.ContinuityContiguous,
		contract.VideoListV1{
			ChannelID: input.Subject(),
			Videos:    []contract.VideoListItemV1{},
			Coverage: contract.ChannelListCoverageV1{
				ChannelID: input.Subject(), MaxResults: 10, Exhausted: true,
			},
		},
	)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("envelope: %w", err)
	}

	output, err := collection.OutputFromEnvelopes([]contract.Envelope{envelope}, time.Now())
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("output from envelopes: %w", err)
	}

	partial, err := collection.NewPartialResult(
		output,
		collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "shorts timeout"),
		contract.KindShortsList,
	)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("new partial result: %w", err)
	}

	return partial, nil
}

var testRetryBounds = collection.RetryBounds{Minimum: 100 * time.Millisecond, Maximum: time.Second}

func runtimeLeaseConfig() joblease.Config {
	return joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		RenewTimeout: 50 * time.Millisecond, DBTimeout: 250 * time.Millisecond, CleanupTimeout: 250 * time.Millisecond,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 4, QueueCapacity: 4,
	}
}

// runtimeCollectorConfig는 runtime 테스트의 scheduler 정책을 lease 설정과 같은 작은 예산으로 맞춥니다.
func runtimeCollectorConfig() collectorconfig.Config {
	cfg := collectorconfig.DefaultConfig()

	cfg.TotalWorkers = 1
	cfg.QueueCapacity = 4
	cfg.AcquisitionBatch = 4
	cfg.AcquisitionCadence = 100 * time.Millisecond
	cfg.RetryMin = testRetryBounds.Minimum
	cfg.RetryMax = testRetryBounds.Maximum

	return cfg
}

func withOverride(overrides ...collection.JobRunner) []collection.JobRunner {
	runners := completeStubRunners()
	for i, runner := range runners {
		for _, override := range overrides {
			if runner.JobID() == override.JobID() {
				runners[i] = override
			}
		}
	}

	return runners
}

func defaultProviderGates() map[contract.Provider]chan struct{} {
	cfg := collectorconfig.DefaultConfig()
	return newProviderGates(&cfg)
}

type leaseSeed struct {
	subject string
	kind    contract.ObservationKind
}

func seedRuntimeCommunityTarget(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	seedRuntimeTargets(t, pool, []leaseSeed{{testSubjectKey, contract.KindCommunityPage}})
}

func seedRuntimeTargets(t *testing.T, pool *pgxpool.Pool, targets []leaseSeed) {
	t.Helper()
	seedRuntimeTargetsEvery(t, pool, time.Minute, targets)
}

func seedRuntimeTargetsEvery(t *testing.T, pool *pgxpool.Pool, interval time.Duration, targets []leaseSeed) {
	t.Helper()

	ctx := t.Context()

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
				priority, poll_interval_ms, enabled, valid_until, member_since_generation
			) VALUES ($1, $2, $3, 50, $4, TRUE, clock_timestamp() + INTERVAL '1 hour', $1)
		`, generation, target.subject, target.kind, interval.Milliseconds()); err != nil {
			t.Fatal(err)
		}
	}
}
