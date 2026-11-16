package collectorruntime

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

const (
	collectorCRole     = "hololive_collector_c"
	collectorCInstance = "youtube-collector-c"
	collectorDRole     = "hololive_collector_d"
	collectorDInstance = "youtube-collector-d"
	metadataJobKind    = "youtubejs_channel_metadata"
)

func TestCollectorSessionTakesOverExpiredLeaseAndPublishes(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindChannelProfile},
		{testSubjectKey, contract.KindChannelPhoto},
	})

	spec := metadataSpec()
	collectorD := dbtest.NewCollectorSessionPool(t, pool, collectorDRole)

	if _, err := newLeaseRepository(t, collectorD).Acquire(ctx, &spec, collectorDInstance); err != nil {
		t.Fatalf("collector d acquire: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE job_key = $1
	`, spec.JobKey); err != nil {
		t.Fatal(err)
	}

	collectorC := dbtest.NewCollectorSessionPool(t, pool, collectorCRole)
	runAdmittedSpec(ctx, t, newIdentityExecutor(t, collectorC, collectorCInstance, metadataRunner()), &spec)

	var (
		observations, queued int
		state                string
		fence                int64
	)

	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM source_observations AS observation
		        JOIN source_observation_provenances AS provenance ON provenance.id = observation.provenance_id
		        WHERE provenance.collector_instance = $2),
		       (SELECT count(*) FROM source_observation_queue),
		       lease.slot_state, lease.fence_epoch
		FROM youtube_collection_job_leases AS lease
		WHERE lease.job_key = $1
	`, spec.JobKey, collectorCInstance).Scan(&observations, &queued, &state, &fence); err != nil {
		t.Fatal(err)
	}

	if observations != 2 || queued != 2 || state != "IDLE" || fence != 2 {
		t.Fatalf("observations=%d queued=%d state=%s fence=%d, want 2/2/IDLE/2", observations, queued, state, fence)
	}
}

func TestCollectorSessionPublishesPartialAndDefers(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindVideoList},
		{testSubjectKey, contract.KindShortsList},
	})

	content := stubJob(contract.ProviderYouTubeJS, "youtubejs_content", contract.KindVideoList, contract.KindShortsList)
	content.collect = collectYouTubeJSPartialVideoList
	spec := joblease.JobSpec{
		JobKey: "collector:youtubejs:youtubejs_content:UC_TEST", Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: "youtubejs_content", SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}

	collectorC := dbtest.NewCollectorSessionPool(t, pool, collectorCRole)
	runAdmittedSpec(ctx, t, newIdentityExecutor(t, collectorC, collectorCInstance, content), &spec)

	var (
		count int
		state string
	)

	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM source_observations WHERE observation_kind = 'video_list'), slot_state
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey).Scan(&count, &state); err != nil {
		t.Fatal(err)
	}

	if count != 1 || state != "DEFERRED" {
		t.Fatalf("partial terminal state count=%d state=%s", count, state)
	}
}

func TestCollectorSessionCannotActForAnotherCollector(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindChannelProfile},
		{testSubjectKey, contract.KindChannelPhoto},
	})

	spec := metadataSpec()
	collectorC := dbtest.NewCollectorSessionPool(t, pool, collectorCRole)
	collectorD := dbtest.NewCollectorSessionPool(t, pool, collectorDRole)

	runAdmittedSpec(ctx, t, newIdentityExecutor(t, collectorC, collectorCInstance, metadataRunner()), &spec)

	var observationID int64
	if err := pool.QueryRow(ctx, `SELECT min(id) FROM source_observations`).Scan(&observationID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases SET next_due_at = clock_timestamp() - INTERVAL '1 second' WHERE job_key = $1
	`, spec.JobKey); err != nil {
		t.Fatal(err)
	}

	lease, err := newLeaseRepository(t, collectorC).Acquire(ctx, &spec, collectorCInstance)
	if err != nil {
		t.Fatalf("collector c reacquire: %v", err)
	}

	if err := lease.Renew(ctx); err != nil {
		t.Fatalf("collector c renew own lease: %v", err)
	}

	for name, run := range map[string]func(*pgxpool.Pool) error{
		"renew another collector's lease": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `
				UPDATE youtube_collection_job_leases SET lease_expires_at = lease_expires_at + INTERVAL '1 minute'
				WHERE job_key = $1`, spec.JobKey)

			return err
		},
		"take over a live lease": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `
				UPDATE youtube_collection_job_leases SET owner_instance = $2, fence_epoch = fence_epoch + 1
				WHERE job_key = $1`, spec.JobKey, collectorDInstance)

			return err
		},
		"insert a lease owned by another collector": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `
				INSERT INTO youtube_collection_job_leases (
					job_key, provider, job_class, collection_job_kind, subject_key, projection_generation,
					poll_interval_ms, slot_state, scheduled_for, next_due_at, fence_epoch, owner_instance,
					lease_expires_at, membership_kinds, membership_exact_subject, membership_target_count)
				SELECT 'collector:youtubejs:youtubejs_channel_metadata:UC_OTHER', provider, job_class, collection_job_kind,
				       'UC_OTHER', projection_generation, poll_interval_ms, 'ACTIVE', scheduled_for, next_due_at, 1, $2,
				       lease_expires_at, membership_kinds, membership_exact_subject, membership_target_count
				FROM youtube_collection_job_leases WHERE job_key = $1`, spec.JobKey, collectorCInstance)

			return err
		},
		"resolve another collector's provenance": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `SELECT source_observation_provenance_id($1, $2, $3)`,
				"collector:youtubejs:youtubejs_channel_metadata:UC_OTHER", collectorCInstance, metadataJobKind)

			return err
		},
		"insert an observation under another collector's provenance": func(session *pgxpool.Pool) error {
			return copyObservation(ctx, session, observationID)
		},
		"queue another collector's observation": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `INSERT INTO source_observation_queue (observation_id) VALUES ($1)`, observationID)

			return err
		},
	} {
		requireInsufficientPrivilege(t, run(collectorD), "collector d "+name)
	}

	requireInsufficientPrivilege(t, copyObservation(ctx, collectorC, observationID), "collector c publish under a released fence")
}

func TestUnboundScraperSessionsCannotWrite(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindChannelProfile},
		{testSubjectKey, contract.KindChannelPhoto},
	})

	legacy := dbtest.NewCollectorSessionPool(t, pool, "hololive_scraper")
	unbound := dbtest.NewCollectorSessionPool(t, pool, "hololive_collector_unbound")

	if _, err := pool.Exec(ctx, `GRANT hololive_scraper TO hololive_collector_unbound WITH INHERIT FALSE`); err != nil {
		t.Fatal(err)
	}

	spec := metadataSpec()

	for name, session := range map[string]*pgxpool.Pool{"legacy scraper": legacy, "unbound group member": unbound} {
		_, err := newLeaseRepository(t, session).Acquire(ctx, &spec, collectorCInstance)
		requireInsufficientPrivilege(t, err, name+" acquire")
	}
}

func metadataSpec() joblease.JobSpec {
	return joblease.JobSpec{
		JobKey: "collector:youtubejs:youtubejs_channel_metadata:UC_TEST", Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: metadataJobKind, SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}
}

func metadataRunner() *stubRunner {
	runner := stubJob(contract.ProviderYouTubeJS, metadataJobKind, contract.KindChannelProfile, contract.KindChannelPhoto)
	runner.collect = collectYouTubeJSMetadata

	return runner
}

func newLeaseRepository(t *testing.T, pool *pgxpool.Pool) *joblease.Repository {
	t.Helper()

	config := runtimeLeaseConfig()

	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	return repository
}

func newIdentityExecutor(t *testing.T, pool *pgxpool.Pool, owner string, runner collection.JobRunner) *collectionExecutor {
	t.Helper()

	registry, err := newTestRegistry(withOverride(runner)...)
	if err != nil {
		t.Fatal(err)
	}

	return &collectionExecutor{
		repository: newLeaseRepository(t, pool), registry: registry, publisher: NewPublisher(pool),
		metrics: NewMetrics(prometheus.NewPedanticRegistry()),
		owner:   owner, logger: slog.New(slog.DiscardHandler), retryBounds: testRetryBounds,
		collector: fixtureCollectorConfig(t),
		gates:     defaultProviderGates(t),
	}
}

func copyObservation(ctx context.Context, session *pgxpool.Pool, observationID int64) error {
	_, err := session.Exec(ctx, `
		INSERT INTO source_observations (
			provider, observation_kind, subject_key, observation_key, schema_version, contract_generation,
			scheduled_for, observed_at, source_event_at, scope_sha256, completeness, continuity,
			payload_id, evidence_sha256, provenance_id, fence_epoch, projection_generation)
		SELECT provider, observation_kind, subject_key, observation_key, schema_version, contract_generation,
		       scheduled_for, observed_at, source_event_at, scope_sha256, completeness, continuity,
		       payload_id, evidence_sha256, provenance_id, fence_epoch, projection_generation
		FROM source_observations WHERE id = $1`, observationID)

	return err
}

func requireInsufficientPrivilege(t *testing.T, err error, action string) {
	t.Helper()

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "42501" {
		t.Fatalf("%s error = %v, want SQLSTATE 42501", action, err)
	}
}
