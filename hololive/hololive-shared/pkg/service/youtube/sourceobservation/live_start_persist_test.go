package sourceobservation

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLiveConsumerHolodexLiveWithoutActualStartStaysUpcoming(t *testing.T) {
	pool, repo, consumer, proof := startHolodexLivePersist(t)
	waitingRoom := liveSession(testVideoID, testStatusLive)

	waitingRoom.ScheduledAt = new(proof.ScheduledFor.Add(time.Hour))

	observationID := publishConsumeLiveFromProvider(
		t.Context(),
		t,
		repo,
		consumer,
		&proof,
		contract.ProviderHolodex,
		testHolodexLiveKey,
		waitingRoom,
	)

	replayLiveObservation(t, repo, consumer, observationID)
	requireUnconfirmedLiveProjection(t, loadLiveStartProjection(t, pool), *waitingRoom.ScheduledAt)
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

	return pool, repo, newLiveTestConsumer(pool, repo, 0), proof
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
		JOIN youtube_live_reconciliation_heads AS head USING (video_id)
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

	pool, repo, consumer, holodexProof := startHolodexLivePersist(t)
	youtubeProof := seedAdditionalLease(
		t,
		pool,
		&holodexProof,
		contract.ProviderYouTubeJS,
		contract.KindLiveSnapshot,
		testChannelID,
		"youtubejs_channel_live",
	)

	for _, provider := range providers {
		proof, subjectKey := liveProviderProof(provider, &holodexProof, &youtubeProof)

		publishConsumeLiveFromProvider(
			t.Context(),
			t,
			repo,
			consumer,
			proof,
			provider,
			subjectKey,
			liveSession(testVideoID, testStatusLive),
		)
	}

	requireConfirmedLiveProjection(t, loadLiveStartProjection(t, pool), youtubeProof.ScheduledFor)
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
