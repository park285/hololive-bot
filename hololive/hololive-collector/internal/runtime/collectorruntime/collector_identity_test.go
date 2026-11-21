package collectorruntime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kapu/hololive-collector/internal/runtime/collection"
	"github.com/kapu/hololive-collector/internal/runtime/joblease"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	collectorCRole     = "hololive_collector_c"
	collectorCInstance = "collector-c"
	collectorDRole     = "hololive_collector_d"
	collectorDInstance = "collector-d"
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

	if _, err := newLeaseRepository(t, collectorD).Acquire(ctx, &spec, collectorOwner(t, collectorDInstance)); err != nil {
		t.Fatalf("collector d acquire: %v", err)
	}

	expireLease(t, pool, spec.JobKey)

	collectorC := dbtest.NewCollectorSessionPool(t, pool, collectorCRole)
	ownerC := collectorOwner(t, collectorCInstance)
	runAdmittedSpec(ctx, t, newIdentityExecutor(t, collectorC, ownerC, metadataRunner()), &spec)

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
	`, spec.JobKey, ownerC).Scan(&observations, &queued, &state, &fence); err != nil {
		t.Fatal(err)
	}

	if observations != 2 || queued != 2 || state != "IDLE" || fence != 2 {
		t.Fatalf("observations=%d queued=%d state=%s fence=%d, want 2/2/IDLE/2", observations, queued, state, fence)
	}
}

func TestCollectorSessionTakesOverPreRenameLeaseOnlyAfterExpiry(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindChannelProfile},
		{testSubjectKey, contract.KindChannelPhoto},
	})

	spec := metadataSpec()
	collectorC := dbtest.NewCollectorSessionPool(t, pool, collectorCRole)
	ownerC := collectorOwner(t, collectorCInstance)

	if _, err := newLeaseRepository(t, collectorC).Acquire(ctx, &spec, ownerC); err != nil {
		t.Fatalf("collector c acquire: %v", err)
	}

	preRenameOwner := "youtube-" + collectorCInstance + ":" + strings.Repeat("0", 32)
	if _, err := pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET owner_instance = $2 WHERE job_key = $1`, spec.JobKey, preRenameOwner); err != nil {
		t.Fatal(err)
	}

	takeover := crossCollectorWrites(ctx, spec.JobKey, 0, preRenameOwner, ownerC)["take over a live lease"]
	requireInsufficientPrivilege(t, takeover(collectorC), "renamed collector takeover of a live pre-rename lease")

	expireLease(t, pool, spec.JobKey)
	runAdmittedSpec(ctx, t, newIdentityExecutor(t, collectorC, ownerC, metadataRunner()), &spec)

	var (
		owned int
		fence int64
	)

	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM source_observation_provenances WHERE collector_instance = $2), fence_epoch
		FROM youtube_collection_job_leases WHERE job_key = $1`, spec.JobKey, ownerC).Scan(&owned, &fence); err != nil {
		t.Fatal(err)
	}

	if owned != 1 || fence != 2 {
		t.Fatalf("renamed collector provenance=%d fence=%d, want 1/2", owned, fence)
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
	runAdmittedSpec(ctx, t, newIdentityExecutor(t, collectorC, collectorOwner(t, collectorCInstance), content), &spec)

	var (
		count    int
		deferred bool
	)

	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM source_observations WHERE observation_kind = 'video_list'), slot_state = 'DEFERRED'
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey).Scan(&count, &deferred); err != nil {
		t.Fatal(err)
	}

	if count != 1 || !deferred {
		t.Fatalf("partial terminal state count=%d deferred=%t", count, deferred)
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

	ownerC := collectorOwner(t, collectorCInstance)
	runAdmittedSpec(ctx, t, newIdentityExecutor(t, collectorC, ownerC, metadataRunner()), &spec)

	var observationID int64

	if err := pool.QueryRow(ctx, `SELECT min(id) FROM source_observations`).Scan(&observationID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_job_leases SET next_due_at = clock_timestamp() - INTERVAL '1 second' WHERE job_key = $1
	`, spec.JobKey); err != nil {
		t.Fatal(err)
	}

	lease, err := newLeaseRepository(t, collectorC).Acquire(ctx, &spec, ownerC)
	if err != nil {
		t.Fatalf("collector c reacquire: %v", err)
	}

	if err := lease.Renew(ctx); err != nil {
		t.Fatalf("collector c renew own lease: %v", err)
	}

	for name, run := range crossCollectorWrites(ctx, spec.JobKey, observationID, ownerC, collectorOwner(t, collectorDInstance)) {
		requireInsufficientPrivilege(t, run(collectorD), "collector d "+name)
	}

	requireInsufficientPrivilege(t, copyObservation(ctx, collectorC, observationID), "collector c publish under a released fence")

	if err := lease.Release(ctx, joblease.ReleaseShutdown); err != nil {
		t.Fatalf("collector c release own lease: %v", err)
	}
}

func TestRestartedCollectorTakesOverItsExpiredLeaseOnlyByAdvancingFence(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindChannelProfile},
		{testSubjectKey, contract.KindChannelPhoto},
	})

	spec := metadataSpec()
	collectorC := dbtest.NewCollectorSessionPool(t, pool, collectorCRole)
	repository := newLeaseRepository(t, collectorC)

	if _, err := repository.Acquire(ctx, &spec, collectorOwner(t, collectorCInstance)); err != nil {
		t.Fatalf("collector c acquire: %v", err)
	}

	expireLease(t, pool, spec.JobKey)

	lease, err := repository.Acquire(ctx, &spec, collectorOwner(t, collectorCInstance))
	if err != nil {
		t.Fatalf("restarted collector c take over its expired lease: %v", err)
	}

	if fence := lease.Proof().FenceEpoch; fence != 2 {
		t.Fatalf("restarted collector c fence = %d, want 2", fence)
	}

	expireLease(t, pool, spec.JobKey)

	_, err = collectorC.Exec(ctx, `UPDATE youtube_collection_job_leases SET owner_instance = $2 WHERE job_key = $1`,
		spec.JobKey, collectorOwner(t, collectorCInstance))
	requireInsufficientPrivilege(t, err, "collector c take over without advancing the fence")
}

func TestCollectorSessionCannotInsertLeaseOutsideItsInstance(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindChannelProfile},
		{testSubjectKey, contract.KindChannelPhoto},
	})

	spec := metadataSpec()
	collectorC := dbtest.NewCollectorSessionPool(t, pool, collectorCRole)

	if _, err := newLeaseRepository(t, collectorC).Acquire(ctx, &spec, collectorOwner(t, collectorCInstance)); err != nil {
		t.Fatalf("collector c acquire: %v", err)
	}

	for name, owner := range map[string]*string{
		"no owner":                   nil,
		"another collector":          new(collectorOwner(t, collectorDInstance)),
		"instance name continuation": new(collectorCInstance + "x:" + strings.Repeat("0", 32)),
		"instance suffix":            new(collectorCInstance + "-x"),
	} {
		requireInsufficientPrivilege(t, insertLeaseCopy(ctx, collectorC, spec.JobKey, owner), "collector c insert lease with "+name)
	}
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

	spec := metadataSpec()

	for name, session := range map[string]*pgxpool.Pool{"legacy scraper": legacy, "unbound group member": unbound} {
		_, err := newLeaseRepository(t, session).Acquire(ctx, &spec, collectorOwner(t, collectorCInstance))
		requireInsufficientPrivilege(t, err, name+" acquire")
	}
}

func crossCollectorWrites(
	ctx context.Context, jobKey string, observationID int64, victimOwner, attackerOwner string,
) map[string]func(*pgxpool.Pool) error {
	return map[string]func(*pgxpool.Pool) error{
		"renew another collector's lease": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `
				UPDATE youtube_collection_job_leases SET lease_expires_at = lease_expires_at + INTERVAL '1 minute'
				WHERE job_key = $1`, jobKey)

			return err
		},
		"take over a live lease": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `
				UPDATE youtube_collection_job_leases SET owner_instance = $2, fence_epoch = fence_epoch + 1
				WHERE job_key = $1`, jobKey, attackerOwner)

			return err
		},
		"insert a lease owned by another collector": func(session *pgxpool.Pool) error {
			return insertLeaseCopy(ctx, session, jobKey, &victimOwner)
		},
		"resolve another collector's provenance": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `SELECT source_observation_provenance_id($1, $2, $3)`,
				"collector:youtubejs:youtubejs_channel_metadata:UC_OTHER", victimOwner, metadataJobKind)

			return err
		},
		"insert an observation under another collector's provenance": func(session *pgxpool.Pool) error {
			return copyObservation(ctx, session, observationID)
		},
		"queue another collector's observation": func(session *pgxpool.Pool) error {
			_, err := session.Exec(ctx, `INSERT INTO source_observation_queue (observation_id) VALUES ($1)`, observationID)

			return err
		},
	}
}

func insertLeaseCopy(ctx context.Context, session *pgxpool.Pool, jobKey string, owner *string) error {
	_, err := session.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key, projection_generation,
			poll_interval_ms, slot_state, scheduled_for, next_due_at, fence_epoch, owner_instance,
			lease_expires_at, membership_kinds, membership_exact_subject, membership_target_count)
		SELECT 'collector:youtubejs:youtubejs_channel_metadata:UC_OTHER', provider, job_class, collection_job_kind,
		       'UC_OTHER', projection_generation, poll_interval_ms, 'ACTIVE', scheduled_for, next_due_at, 1, $2,
		       lease_expires_at, membership_kinds, membership_exact_subject, membership_target_count
		FROM youtube_collection_job_leases WHERE job_key = $1`, jobKey, owner)

	return err
}

func collectorOwner(t *testing.T, instance string) string {
	t.Helper()

	owner, err := newCollectorOwner(instance)
	if err != nil {
		t.Fatal(err)
	}

	return owner
}

func expireLease(t *testing.T, pool *pgxpool.Pool, jobKey string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		UPDATE youtube_collection_job_leases SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE job_key = $1
	`, jobKey); err != nil {
		t.Fatal(err)
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
