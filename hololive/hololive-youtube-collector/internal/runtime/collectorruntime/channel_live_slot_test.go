package collectorruntime

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	consumekit "github.com/kapu/hololive-api/testkit/sourceobservation"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejscollector"
)

const channelLiveSlotRounds = 3

// 방송 탭 snapshot이 같은 슬롯에서 계속 실패·DEFERRED여도 채널 확인 job은 cadence마다 자기 슬롯을 완료하고
// 새 scheduled_for·observation key로 발행되어 canonical 채널 확인을 신선하게 갱신한다.
func TestChannelLiveCheckSlotAdvancesWhileSnapshotRetries(t *testing.T) {
	ctx := t.Context()
	pool, executor, consumer, client := newChannelLiveSlotFixture(t)
	snapshotSpec := channelLiveSlotSpec("youtubejs_channel_live")
	checkSpec := channelLiveSlotSpec("youtubejs_channel_live_check")

	var (
		snapshotSlot, previousCheckSlot time.Time
		keys                            = make(map[string]struct{}, channelLiveSlotRounds)
	)

	for round := 1; round <= channelLiveSlotRounds; round++ {
		waitLeaseDue(t, pool, snapshotSpec.JobKey)
		executor.runSpec(ctx, &snapshotSpec)
		waitLeaseDue(t, pool, checkSpec.JobKey)
		executor.runSpec(ctx, &checkSpec)

		if err := consumer.Consume(ctx, consumekit.ClaimOptions{
			ConsumerName: "youtube-live-processor", LeaseOwner: "api-a",
			Kinds: []contract.ObservationKind{contract.KindChannelLiveCheck}, Limit: 10, LeaseDuration: 30 * time.Second,
		}); err != nil {
			t.Fatalf("round %d consume: %v", round, err)
		}

		snapshot := loadChannelLiveSlot(t, pool, snapshotSpec.JobKey)
		check := loadChannelLiveSlot(t, pool, checkSpec.JobKey)
		published := loadLatestChannelLiveCheckObservation(t, pool)
		canonical := loadCanonicalChannelLiveCheck(t, pool)

		t.Logf("round %d snapshot lease: state=%s scheduled_for=%s fence=%d completed=%s failure=%s",
			round, snapshot.state, snapshot.scheduledFor.Format(time.RFC3339Nano), snapshot.fenceEpoch, optionalTime(snapshot.lastCompletedAt), snapshot.failureCode)
		t.Logf("round %d check lease: state=%s scheduled_for=%s fence=%d completed=%s",
			round, check.state, check.scheduledFor.Format(time.RFC3339Nano), check.fenceEpoch, optionalTime(check.lastCompletedAt))
		t.Logf("round %d observation: count=%d id=%d key=%s scheduled_for=%s",
			round, published.count, published.id, published.key, published.scheduledFor.Format(time.RFC3339Nano))
		t.Logf("round %d canonical: outcome=%s observation_id=%d effective_at=%s received_age=%s",
			round, canonical.outcome, canonical.observationID, canonical.effectiveAt.Format(time.RFC3339Nano), canonical.receivedAge)

		if round == 1 {
			snapshotSlot = snapshot.scheduledFor
		}

		assertIndependentLeaseStates(t, round, &snapshot, &check, snapshotSlot, previousCheckSlot)
		assertCanonicalCheck(t, round, check.scheduledFor, &published, &canonical, keys)

		previousCheckSlot = check.scheduledFor
	}

	var snapshots int64

	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM source_observations WHERE observation_kind = 'live_snapshot'
	`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}

	if snapshots != 0 || client.channelCalls.Load() != channelLiveSlotRounds || client.checkCalls.Load() != channelLiveSlotRounds {
		t.Fatalf("snapshots=%d channel calls=%d check calls=%d", snapshots, client.channelCalls.Load(), client.checkCalls.Load())
	}
}

func newChannelLiveSlotFixture(t *testing.T) (*pgxpool.Pool, *collectionExecutor, *consumekit.Consumer, *splitChannelLiveClient) {
	t.Helper()

	pool := dbtest.NewPool(t)
	seedRuntimeTargetsEvery(t, pool, time.Second, []leaseSeed{
		{testSubjectKey, contract.KindLiveSnapshot},
		{testSubjectKey, contract.KindChannelLiveCheck},
	})

	config := runtimeLeaseConfig()

	repository, err := joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	client := &splitChannelLiveClient{}

	registry, err := newTestRegistry(withOverride(
		youtubejscollector.NewChannelLiveRunner(client),
		youtubejscollector.NewChannelLiveCheckRunner(client),
	)...)
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
	consumer := consumekit.NewConsumer(pool, 0, 0)

	return pool, executor, consumer, client
}

func assertIndependentLeaseStates(t *testing.T, round int, snapshot, check *channelLiveSlot, snapshotSlot, previousCheckSlot time.Time) {
	t.Helper()

	// 방송 탭은 같은 슬롯에서 재시도만 반복하고 완료되지 않는다.
	if snapshot.state != "DEFERRED" || !snapshot.scheduledFor.Equal(snapshotSlot) || snapshot.fenceEpoch != int64(round) ||
		snapshot.lastCompletedAt != nil || snapshot.failureCode != "collection_timeout" {
		t.Fatalf("round %d snapshot lease = %+v, want DEFERRED retry of slot %s", round, snapshot, snapshotSlot)
	}

	// 채널 확인은 매 cadence 새 슬롯을 완료한다.
	if check.state != "IDLE" || check.lastCompletedAt == nil || !check.scheduledFor.After(previousCheckSlot) ||
		!check.scheduledFor.After(snapshotSlot) {
		t.Fatalf("round %d check lease = %+v, want COMPLETE slot after %s", round, check, previousCheckSlot)
	}
}

func assertCanonicalCheck(t *testing.T, round int, slot time.Time, published *channelLiveCheckObservation, canonical *canonicalChannelLiveCheck, keys map[string]struct{}) {
	t.Helper()

	if _, seen := keys[published.key]; seen || published.count != int64(round) || !published.scheduledFor.Equal(slot) {
		t.Fatalf("round %d observation = %+v, want new key for slot %s", round, published, slot)
	}

	keys[published.key] = struct{}{}

	// canonical은 이번 슬롯의 음성 확인이며 DB 기준 신선하다.
	if canonical.outcome != string(contract.ChannelLiveCheckChannelPage) || canonical.observationID != published.id ||
		!canonical.scheduledFor.Equal(slot) || !canonical.effectiveAt.Equal(slot) ||
		canonical.receivedAge < 0 || canonical.receivedAge > 5*time.Second {
		t.Fatalf("round %d canonical channel live check = %+v, want fresh slot %s observation %d", round, canonical, slot, published.id)
	}
}

func channelLiveSlotSpec(jobKind string) joblease.JobSpec {
	return joblease.JobSpec{
		JobKey:   "collector:youtubejs:" + jobKind + ":" + testSubjectKey,
		Provider: contract.ProviderYouTubeJS, Class: string(collection.JobClassSubject),
		CollectionJobKind: jobKind, SubjectKey: testSubjectKey, PollInterval: time.Second,
	}
}

// waitLeaseDue는 lease가 없거나 다음 슬롯·재시도 시각이 DB 기준으로 도래할 때까지 기다린다.
func waitLeaseDue(t *testing.T, pool *pgxpool.Pool, jobKey string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		var due bool

		if err := pool.QueryRow(t.Context(), `
			SELECT NOT EXISTS (SELECT 1 FROM youtube_collection_job_leases WHERE job_key = $1)
			    OR EXISTS (
			        SELECT 1 FROM youtube_collection_job_leases
			        WHERE job_key = $1
			          AND ((slot_state = 'IDLE' AND next_due_at <= clock_timestamp())
			            OR (slot_state = 'DEFERRED' AND retry_not_before <= clock_timestamp()))
			    )
		`, jobKey).Scan(&due); err != nil {
			t.Fatal(err)
		}

		if due {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("lease %s did not become due", jobKey)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func optionalTime(value *time.Time) string {
	if value == nil {
		return "never"
	}

	return value.Format(time.RFC3339Nano)
}

type channelLiveSlot struct {
	state           string
	scheduledFor    time.Time
	fenceEpoch      int64
	lastCompletedAt *time.Time
	failureCode     string
}

func loadChannelLiveSlot(t *testing.T, pool *pgxpool.Pool, jobKey string) channelLiveSlot {
	t.Helper()

	var slot channelLiveSlot

	if err := pool.QueryRow(t.Context(), `
		SELECT slot_state, scheduled_for, fence_epoch, last_completed_at, COALESCE(last_failure_code, '')
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, jobKey).Scan(&slot.state, &slot.scheduledFor, &slot.fenceEpoch, &slot.lastCompletedAt, &slot.failureCode); err != nil {
		t.Fatalf("load lease %s: %v", jobKey, err)
	}

	return slot
}

type channelLiveCheckObservation struct {
	count        int64
	id           int64
	key          string
	scheduledFor time.Time
}

func loadLatestChannelLiveCheckObservation(t *testing.T, pool *pgxpool.Pool) channelLiveCheckObservation {
	t.Helper()

	var observation channelLiveCheckObservation

	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) OVER (), id, observation_key, scheduled_for
		FROM source_observations
		WHERE observation_kind = 'channel_live_check' AND subject_key = $1
		ORDER BY id DESC
		LIMIT 1
	`, testSubjectKey).Scan(&observation.count, &observation.id, &observation.key, &observation.scheduledFor); err != nil {
		t.Fatalf("load channel live check observation: %v", err)
	}

	return observation
}

type canonicalChannelLiveCheck struct {
	outcome       string
	observationID int64
	scheduledFor  time.Time
	effectiveAt   time.Time
	receivedAge   time.Duration
}

func loadCanonicalChannelLiveCheck(t *testing.T, pool *pgxpool.Pool) canonicalChannelLiveCheck {
	t.Helper()

	var (
		row         canonicalChannelLiveCheck
		receivedAge float64
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT outcome, observation_id, scheduled_for, effective_at,
		       EXTRACT(EPOCH FROM statement_timestamp() - received_at)::double precision
		FROM youtube_channel_live_checks WHERE channel_id = $1
	`, testSubjectKey).Scan(&row.outcome, &row.observationID, &row.scheduledFor, &row.effectiveAt, &receivedAge); err != nil {
		t.Fatalf("load canonical channel live check: %v", err)
	}

	row.receivedAge = time.Duration(receivedAge * float64(time.Second))

	return row
}

// splitChannelLiveClient는 방송 탭 조회를 항상 실패시키고 채널 /live 확인은 요청 채널의 채널 페이지로 응답한다.
type splitChannelLiveClient struct {
	channelCalls atomic.Int64
	checkCalls   atomic.Int64
}

func (c *splitChannelLiveClient) FetchChannel(context.Context, youtubejs.ChannelRequest) (youtubejs.ChannelResult, error) {
	c.channelCalls.Add(1)

	return youtubejs.ChannelResult{}, collecterr.New(collecterr.Timeout, collecterr.ClassTimeout, "channel tab timeout")
}

func (c *splitChannelLiveClient) FetchChannelLiveCheck(
	_ context.Context,
	request youtubejs.ChannelLiveCheckRequest,
) (youtubejs.ChannelLiveCheckResult, error) {
	c.checkCalls.Add(1)

	return youtubejs.ChannelLiveCheckResult{
		ChannelID: request.ChannelID, Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true,
	}, nil
}
