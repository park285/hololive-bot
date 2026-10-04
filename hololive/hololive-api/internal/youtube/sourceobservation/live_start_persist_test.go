package sourceobservation

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func TestLiveConsumerHolodexLiveWithoutActualStartStaysUpcoming(t *testing.T) {
	pool, repo, consumer, proof := startHolodexLivePersist(t)
	waitingRoom := liveSession(testVideoID, testStatusLive)

	waitingRoom.ScheduledAt = new(proof.ScheduledFor.Add(time.Hour))

	observationID := publishConsumeLiveFromProvider(
		t.Context(),
		t,
		publishkit.NewPublisher(pool),
		consumer,
		&proof,
		contract.ProviderHolodex,
		testHolodexLiveKey,
		waitingRoom,
	)

	replayLiveObservation(t, repo, consumer, observationID)
	requireUnconfirmedLiveProjection(t, loadLiveStartProjection(t, pool), *waitingRoom.ScheduledAt)
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
	assertLifecycleOrigin(t, pool, "metadata_only")
	requireLiveApplicationDecision(t, pool, observationID, "LIVE_START_UNCONFIRMED")
}

func TestLiveConsumerStartEvidenceConvergesAcrossProviderOrder(t *testing.T) {
	orders := []struct {
		name      string
		providers []contract.Provider
	}{
		{name: "Holodex then YouTube", providers: []contract.Provider{contract.ProviderHolodex, contract.ProviderYouTubeJS}},
		{name: "YouTube then Holodex", providers: []contract.Provider{contract.ProviderYouTubeJS, contract.ProviderHolodex}},
	}

	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			requireLiveStartEvidenceConvergence(t, order.providers)
		})
	}
}

type liveStartProjection struct {
	status          string
	scheduledAt     *time.Time
	startedAt       *time.Time
	liveFirstSeenAt *time.Time
	lastLiveAt      *time.Time
}

func startHolodexLivePersist(t *testing.T) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof) {
	t.Helper()

	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(
		t.Context(),
		t,
		pool,
		contract.ProviderHolodex,
		contract.KindLiveSnapshot,
		testHolodexLiveKey,
		"holodex_live",
	)

	return pool, repo, NewConsumerWithGraces(repo, 0, 0), proof
}

func replayLiveObservation(t *testing.T, repo *Repository, consumer *Consumer, observationID int64) {
	t.Helper()

	replay, err := repo.RequestReplay(t.Context(), ReplayInput{
		ObservationID: observationID,
		RequestedBy:   testReplayOperator,
		Reason:        "verify unconfirmed live admission replay",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request waiting-room replay: replay=%#v err=%v", replay, err)
	}

	if err := consumer.Consume(t.Context(), liveClaimOptions()); err != nil {
		t.Fatalf("consume waiting-room replay: %v", err)
	}
}

func loadLiveStartProjection(t *testing.T, pool *pgxpool.Pool) liveStartProjection {
	t.Helper()

	var projection liveStartProjection

	if err := pool.QueryRow(t.Context(), `
		SELECT session.status, session.scheduled_start_time, session.started_at,
		       session.live_first_seen_at, head.last_live_positive_at
		FROM youtube_live_sessions AS session
		LEFT JOIN youtube_live_reconciliation_heads AS head USING (video_id)
		WHERE session.video_id = $1
	`, testVideoID).Scan(
		&projection.status,
		&projection.scheduledAt,
		&projection.startedAt,
		&projection.liveFirstSeenAt,
		&projection.lastLiveAt,
	); err != nil {
		t.Fatalf("load live-start projection: %v", err)
	}

	return projection
}

func requireUnconfirmedLiveProjection(t *testing.T, projection liveStartProjection, scheduledAt time.Time) {
	t.Helper()

	if projection.status != string(domain.LiveStatusUpcoming) {
		t.Fatalf("status = %s, want UPCOMING", projection.status)
	}

	if projection.scheduledAt == nil || !projection.scheduledAt.Equal(scheduledAt) {
		t.Fatalf("scheduled start = %v, want %s", projection.scheduledAt, scheduledAt.UTC())
	}

	if projection.startedAt != nil || projection.liveFirstSeenAt != nil || projection.lastLiveAt != nil {
		t.Fatalf(
			"unconfirmed Holodex LIVE advanced start: started=%v first_seen=%v positive=%v",
			projection.startedAt,
			projection.liveFirstSeenAt,
			projection.lastLiveAt,
		)
	}
}

func requireLiveApplicationDecision(
	t *testing.T,
	pool *pgxpool.Pool,
	observationID int64,
	want string,
) {
	t.Helper()

	var decision string

	if err := pool.QueryRow(t.Context(), `
		SELECT decision
		FROM source_observation_applications
		WHERE observation_id = $1
		  AND entity_kind = 'youtube_live_session'
		  AND entity_key = $2
	`, observationID, testVideoID).Scan(&decision); err != nil {
		t.Fatalf("load live application: %v", err)
	}

	if decision != want {
		t.Fatalf("application decision = %s, want %s", decision, want)
	}
}

func requireLiveStartEvidenceConvergence(t *testing.T, providers []contract.Provider) {
	t.Helper()

	pool, _, consumer, holodexProof := startHolodexLivePersist(t)
	youtubeProof := seedAdditionalLease(
		t,
		pool,
		&holodexProof,
		contract.KindLiveSnapshot,
		testChannelID,
		"youtubejs_channel_live",
	)

	finalizeLeaseRosterCount(t, pool, &holodexProof)

	for _, provider := range providers {
		proof, subjectKey := liveProviderProof(provider, &holodexProof, &youtubeProof)

		publishConsumeLiveFromProvider(
			t.Context(),
			t,
			publishkit.NewPublisher(pool),
			consumer,
			proof,
			provider,
			subjectKey,
			liveSession(testVideoID, testStatusLive),
		)
	}

	requireConfirmedLiveProjection(t, loadLiveStartProjection(t, pool), youtubeProof.ScheduledFor)
}

// finalizeLeaseRosterCount는 이미 잡은 lease의 membership 수를 현재 projection의 실제 범위로 확정한다.
// Holodex live lease는 subject를 고정하지 않는 global live_snapshot 범위이므로, 실제 acquire라면 함께 등록된
// channel live target까지 세었다. 이 fixture는 target을 lease 뒤에 추가하므로 그 acquisition 명단을 여기서 반영한다.
func finalizeLeaseRosterCount(t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) {
	t.Helper()

	tag, err := pool.Exec(t.Context(), `
		UPDATE youtube_collection_job_leases AS job
		SET membership_target_count = (
			SELECT count(*)
			FROM youtube_collection_targets AS target
			WHERE target.projection_generation = job.projection_generation
			  AND target.observation_kind = ANY(job.membership_kinds)
			  AND target.enabled
			  AND target.valid_until > NOW()
			  AND (NOT job.membership_exact_subject OR target.subject_key = job.subject_key)
		)
		WHERE job.job_key = $1
	`, proof.JobKey)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("finalize lease roster count for %s: rows=%d err=%v", proof.JobKey, tag.RowsAffected(), err)
	}
}

func liveProviderProof(
	provider contract.Provider,
	holodex, youtube *contract.LeaseProof,
) (*contract.LeaseProof, string) {
	if provider == contract.ProviderHolodex {
		return holodex, testHolodexLiveKey
	}

	return youtube, testChannelID
}

func requireConfirmedLiveProjection(t *testing.T, projection liveStartProjection, startedAt time.Time) {
	t.Helper()

	if projection.status != string(domain.LiveStatusLive) {
		t.Fatalf("status = %s, want LIVE", projection.status)
	}

	if projection.startedAt == nil || !projection.startedAt.Equal(startedAt) {
		t.Fatalf("started at = %v, want %s", projection.startedAt, startedAt.UTC())
	}

	if projection.lastLiveAt == nil || !projection.lastLiveAt.Equal(startedAt) {
		t.Fatalf("last live positive = %v, want %s", projection.lastLiveAt, startedAt.UTC())
	}
}
