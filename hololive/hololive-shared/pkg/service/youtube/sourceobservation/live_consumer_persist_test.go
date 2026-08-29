package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestLiveConsumerUpcomingLiveEndedPersistsOnce(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "UPCOMING"))
	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))
	publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "ENDED"))

	if status := liveSessionStatus(t, pool); status != string(domain.LiveStatusEnded) {
		t.Fatalf("status = %s, want ENDED", status)
	}

	assertTableCount(t, pool, "youtube_notification_outbox", 0)
	assertTableCount(t, pool, "youtube_live_sessions", 1)
}

func TestLiveConsumerPersistsGenerationTwoMetadataAndPreservesSparseFields(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `
		UPDATE observation_contract_generations
		SET current_generation = $1
		WHERE provider = 'youtubejs' AND observation_kind = 'live_snapshot'
	`, contract.LiveSnapshotMetadataContractGeneration); err != nil {
		t.Fatalf("bump live snapshot contract: %v", err)
	}

	metadata := liveSession(testVideoID, "UPCOMING")

	metadata.Title = "Minecraft live"
	metadata.TopicID = "minecraft"
	metadata.ThumbnailURL = "https://i.ytimg.com/vi/vid-a/maxresdefault.jpg"

	proof = publishConsumeLiveAtGeneration(
		ctx,
		t,
		pool,
		repo,
		consumer,
		&proof,
		contract.LiveSnapshotMetadataContractGeneration,
		metadata,
	)
	publishConsumeLiveAtGeneration(
		ctx,
		t,
		pool,
		repo,
		consumer,
		&proof,
		contract.LiveSnapshotMetadataContractGeneration,
		liveSession(testVideoID, testStatusLive),
	)

	var title, topicID, thumbnailURL string

	if err := pool.QueryRow(ctx, `
		SELECT title, topic_id, thumbnail_url
		FROM youtube_live_sessions
		WHERE video_id = $1
	`, testVideoID).Scan(&title, &topicID, &thumbnailURL); err != nil {
		t.Fatalf("load live metadata: %v", err)
	}

	if title != metadata.Title || topicID != metadata.TopicID || thumbnailURL != metadata.ThumbnailURL {
		t.Fatalf("persisted metadata = {%q, %q, %q}", title, topicID, thumbnailURL)
	}
}

func TestLiveConsumerDoesNotRewriteUntouchedSession(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	lastSeen := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, last_seen_at)
		VALUES ('vid-keep', 'UC_TEST', 'LIVE', 'Keep', $1)
	`, lastSeen); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindLiveSnapshot, testChannelID, "youtubejs_channel_live")
	consumer := newLiveTestConsumer(pool, repo, 0)

	if _, err := repo.PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, &proof, liveSession("vid-new", testStatusLive)))); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}

	var (
		title string
		seen  time.Time
	)

	if err := pool.QueryRow(ctx, `SELECT title, last_seen_at FROM youtube_live_sessions WHERE video_id = 'vid-keep'`).Scan(&title, &seen); err != nil {
		t.Fatal(err)
	}

	if title != "Keep" || !seen.Equal(lastSeen) {
		t.Fatalf("untouched session rewritten: %s %s", title, seen)
	}
}

func TestLiveConsumerPreservesPremiereClassification(t *testing.T) {
	tests := []struct {
		name       string
		isPremiere bool
	}{
		{name: "confirmed Premiere", isPremiere: true},
		{name: "confirmed non-Premiere", isPremiere: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool, repo, consumer, proof := startLivePersist(t)
			ctx := t.Context()
			seen := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)

			if _, err := pool.Exec(ctx, `
				INSERT INTO youtube_live_sessions (
					video_id, channel_id, status, title, last_seen_at, is_premiere
				) VALUES ($1, $2, 'UPCOMING', 'Keep classification', $3, $4)
			`, testVideoID, testChannelID, seen, test.isPremiere); err != nil {
				t.Fatalf("seed classified session: %v", err)
			}

			publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))

			assertLiveSessionPremiere(t, pool, domain.LiveStatusLive, new(test.isPremiere))
		})
	}
}

func TestLiveConsumerStaleEpochCannotCreateEndEvidence(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))

	stale := proof

	stale.FenceEpoch = proof.FenceEpoch - 1
	stale.ScheduledFor = proof.ScheduledFor.Add(-time.Minute)

	_, err := repo.PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, &stale)))
	if err == nil {
		t.Fatal("stale epoch publish must fail closed")
	}

	if liveSessionStatus(t, pool) != string(domain.LiveStatusLive) {
		t.Fatal("stale publish must not end the session")
	}
}

func TestLiveConsumerFutureSkewedSourceEventUsesScheduledSlot(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()
	envelope := liveSnapshotEnvelope(t, &proof, liveSession(testVideoID, testStatusLive))
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	envelope.SourceEventAt = &future

	prepared, err := contract.PrepareEnvelope(*envelope)
	if err != nil {
		t.Fatalf("prepare skewed envelope: %v", err)
	}

	if _, publishErr := repo.PublishBatch(ctx, publishInput(&prepared)); publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}

	if consumeErr := consumer.Consume(ctx, liveClaimOptions()); consumeErr != nil {
		t.Fatalf("consume: %v", consumeErr)
	}

	var effective time.Time

	err = pool.QueryRow(ctx, `
		SELECT last_live_positive_at FROM youtube_live_reconciliation_heads WHERE video_id = 'vid-a'
	`).Scan(&effective)
	if err != nil {
		var status, detail string

		if diagnosticErr := pool.QueryRow(ctx, `SELECT status, COALESCE(last_error_detail, '') FROM source_observation_queue LIMIT 1`).Scan(&status, &detail); diagnosticErr != nil {
			t.Fatalf("load live head: %v; load queue diagnostic: %v", err, diagnosticErr)
		}

		t.Fatalf("load live head: %v queue=%s detail=%s", err, status, detail)
	}

	if !effective.UTC().Equal(proof.ScheduledFor.UTC()) {
		t.Fatalf("effective = %s, want scheduled %s", effective.UTC(), proof.ScheduledFor.UTC())
	}
}

func TestLiveConsumerTwoScopedAbsenceSlotsAfterGraceEnd(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))
	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof)
	publishConsumeLive(ctx, t, pool, repo, consumer, &proof)

	if liveSessionStatus(t, pool) != string(domain.LiveStatusEnded) {
		t.Fatal("two distinct scoped absence slots after grace must end")
	}
}

func TestLiveConsumerOneScopedAbsenceDoesNotEnd(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))
	publishConsumeLive(ctx, t, pool, repo, consumer, &proof)

	if liveSessionStatus(t, pool) != string(domain.LiveStatusLive) {
		t.Fatal("one scoped absence must not end")
	}

	if slots := liveConsecutiveSlots(t, pool, testVideoID); slots != 1 {
		t.Fatalf("consecutive = %d, want 1", slots)
	}
}

func TestLiveConsumerNeverLiveUpcomingScopedAbsenceDoesNotEnd(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "UPCOMING"))
	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof)
	publishConsumeLive(ctx, t, pool, repo, consumer, &proof)

	if liveSessionStatus(t, pool) != string(domain.LiveStatusUpcoming) {
		t.Fatal("scoped absence must not end never-live UPCOMING")
	}
}

func TestLiveConsumerAlreadyEndedLateLiveStaysEnded(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))
	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "ENDED"))

	seen := liveLastSeen(t, pool)
	publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))

	if liveSessionStatus(t, pool) != string(domain.LiveStatusEnded) {
		t.Fatal("already ENDED session must stay ENDED")
	}

	if !liveLastSeen(t, pool).Equal(seen) {
		t.Fatal("late LIVE must not rewrite an ENDED session")
	}
}

func TestLiveConsumerDoesNotStubOverwriteExistingLiveHead(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))

	var liveAt time.Time

	if err := pool.QueryRow(ctx, `
		SELECT last_live_positive_at FROM youtube_live_reconciliation_heads WHERE video_id = 'vid-a'
	`).Scan(&liveAt); err != nil {
		t.Fatal(err)
	}

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "ENDED"))
	publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession("vid-new", testStatusLive))

	var (
		status string
		kept   time.Time
	)

	if err := pool.QueryRow(ctx, `
		SELECT status, last_live_positive_at FROM youtube_live_reconciliation_heads WHERE video_id = 'vid-a'
	`).Scan(&status, &kept); err != nil {
		t.Fatal(err)
	}

	if status != string(domain.LiveStatusLive) {
		t.Fatalf("existing LIVE head overwritten to %s", status)
	}

	if !kept.Equal(liveAt) {
		t.Fatalf("LIVE clocks wiped: %s -> %s", liveAt, kept)
	}
}

func TestLiveEndFinalizerMissRequeuesOrClearsDueRow(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))
	publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "ENDED"))

	if liveSessionStatus(t, pool) != string(domain.LiveStatusLive) {
		t.Fatal("explicit end before grace must stay LIVE")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_seen_at = NOW(),
		    next_end_check_at = NOW()
		WHERE video_id = 'vid-a'
	`); err != nil {
		t.Fatalf("force due miss: %v", err)
	}

	processed, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
	if err != nil || !processed {
		t.Fatalf("finalize due miss: processed=%t err=%v", processed, err)
	}

	if liveSessionStatus(t, pool) != string(domain.LiveStatusLive) {
		t.Fatal("due miss must not end before grace")
	}

	var next *time.Time

	if scanErr := pool.QueryRow(ctx, `
		SELECT next_end_check_at FROM youtube_live_reconciliation_heads WHERE video_id = 'vid-a'
	`).Scan(&next); scanErr != nil {
		t.Fatal(scanErr)
	}

	if next != nil && !next.After(time.Now().Add(-time.Second)) {
		t.Fatalf("due miss left a past next_end_check_at: %s", next.UTC())
	}

	again, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
	if err != nil {
		t.Fatalf("second finalize: %v", err)
	}

	if again {
		t.Fatal("due miss must not livelock the same past next_end_check_at")
	}
}

func TestFinalizerPreservesPremiereWhenEndingAfterGraceWithoutNewObservation(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, testStatusLive))
	if _, err := pool.Exec(ctx, `
		UPDATE youtube_live_sessions SET is_premiere = TRUE WHERE video_id = $1
	`, testVideoID); err != nil {
		t.Fatalf("classify Premiere session: %v", err)
	}

	publishConsumeLive(ctx, t, pool, repo, consumer, &proof, liveSession(testVideoID, "ENDED"))

	if liveSessionStatus(t, pool) != string(domain.LiveStatusLive) {
		t.Fatal("explicit end before grace must stay LIVE")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_seen_at = NOW() - INTERVAL '2 hours',
		    next_end_check_at = NOW()
		WHERE video_id = 'vid-a'
	`); err != nil {
		t.Fatalf("backdate grace: %v", err)
	}

	processed, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
	if err != nil || !processed {
		t.Fatalf("finalize due: processed=%t err=%v", processed, err)
	}

	if liveSessionStatus(t, pool) != string(domain.LiveStatusEnded) {
		t.Fatal("due finalizer must end after grace")
	}

	assertLiveSessionPremiere(t, pool, domain.LiveStatusEnded, new(true))
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
}

func startLivePersist(t *testing.T) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof) {
	t.Helper()

	return startLivePersistGrace(t, 0)
}

func startLivePersistGrace(t *testing.T, grace time.Duration) (*pgxpool.Pool, *Repository, *Consumer, contract.LeaseProof) {
	t.Helper()

	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindLiveSnapshot, testChannelID, "youtubejs_channel_live")

	return pool, repo, newLiveTestConsumer(pool, repo, grace), proof
}

func newLiveTestConsumer(pool *pgxpool.Pool, repo *Repository, grace time.Duration) *Consumer {
	return NewConsumerWithGraces(repo, NewBatchCanonicalWriter(batchrepo.NewPgxBatchRepositoryWithPersister(pool, nil)), nil, 0, grace)
}

func liveClaimOptions() ClaimOptions {
	return ClaimOptions{
		ConsumerName:  "youtube-live-processor",
		LeaseOwner:    "api-a",
		Kinds:         []contract.ObservationKind{contract.KindLiveSnapshot, contract.KindViewerSample, contract.KindSchedule},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}
}

func liveSession(videoID, status string) contract.LiveSessionV1 {
	return contract.LiveSessionV1{VideoID: videoID, ChannelID: testChannelID, Status: status}
}

func liveSnapshotEnvelope(t *testing.T, proof *contract.LeaseProof, sessions ...contract.LiveSessionV1) *contract.Envelope {
	t.Helper()

	return liveSnapshotEnvelopeAtGeneration(t, proof, 1, sessions...)
}

func liveSnapshotEnvelopeAtGeneration(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	sessions ...contract.LiveSessionV1,
) *contract.Envelope {
	t.Helper()

	return liveSnapshotEnvelopeFromProviderAtGeneration(
		t,
		proof,
		generation,
		contract.ProviderYouTubeJS,
		testChannelID,
		sessions...,
	)
}

func liveSnapshotEnvelopeFromProviderAtGeneration(
	t *testing.T,
	proof *contract.LeaseProof,
	generation int64,
	provider contract.Provider,
	subjectKey string,
	sessions ...contract.LiveSessionV1,
) *contract.Envelope {
	t.Helper()

	statuses := make([]string, 0, len(sessions))
	seen := map[string]struct{}{}

	for _, session := range sessions {
		if _, ok := seen[session.Status]; ok {
			continue
		}

		seen[session.Status] = struct{}{}
		statuses = append(statuses, session.Status)
	}

	if len(statuses) == 0 {
		statuses = []string{testStatusLive, "UPCOMING", "ENDED", "CANCELLED"} //nolint:misspell // YouTube 방송 상태 계약값이 영국식 CANCELLED라, canceled로 바꾸면 상태 판정이 어긋난다.
	}

	payload, err := contract.MarshalPayloadV1(contract.LiveSnapshotV1{
		Sessions: sessions,
		Coverage: contract.GlobalChannelCoverageV1{
			RequestedChannelIDs: []string{testChannelID},
			Filters:             contract.LiveFiltersV1{Statuses: statuses},
		},
	})
	if err != nil {
		t.Fatalf("marshal live payload: %v", err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: provider, ObservationKind: contract.KindLiveSnapshot, SubjectKey: subjectKey,
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: generation,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare live envelope: %v", err)
	}

	return &envelope
}

func publishConsumeLiveFromProvider(
	ctx context.Context,
	t *testing.T,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	provider contract.Provider,
	subjectKey string,
	sessions ...contract.LiveSessionV1,
) int64 {
	t.Helper()

	envelope := liveSnapshotEnvelopeFromProviderAtGeneration(t, proof, 1, provider, subjectKey, sessions...)

	published, err := repo.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish %s live: %v", provider, err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume %s live: %v", provider, err)
	}

	return published.Results[0].ObservationID
}

func publishConsumeLive(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	sessions ...contract.LiveSessionV1,
) contract.LeaseProof {
	t.Helper()

	return publishConsumeLiveAtGeneration(ctx, t, pool, repo, consumer, proof, 1, sessions...)
}

func publishConsumeLiveAtGeneration(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	generation int64,
	sessions ...contract.LiveSessionV1,
) contract.LeaseProof {
	t.Helper()

	if _, err := repo.PublishBatch(ctx, publishInput(liveSnapshotEnvelopeAtGeneration(t, proof, generation, sessions...))); err != nil {
		t.Fatalf("publish live: %v", err)
	}

	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume live: %v", err)
	}

	return advanceLease(ctx, t, pool, proof, time.Minute)
}

func liveSessionStatus(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var status string

	if err := pool.QueryRow(t.Context(), `SELECT status FROM youtube_live_sessions WHERE video_id = 'vid-a'`).Scan(&status); err != nil {
		t.Fatalf("load live status: %v", err)
	}

	return status
}

func liveLastSeen(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()

	var seen time.Time

	if err := pool.QueryRow(t.Context(), `SELECT last_seen_at FROM youtube_live_sessions WHERE video_id = 'vid-a'`).Scan(&seen); err != nil {
		t.Fatalf("load last_seen_at: %v", err)
	}

	return seen
}

func liveConsecutiveSlots(t *testing.T, pool *pgxpool.Pool, videoID string) int {
	t.Helper()

	var slots int

	if err := pool.QueryRow(t.Context(), `
		SELECT consecutive_absence_slots FROM youtube_live_reconciliation_heads WHERE video_id = $1
	`, videoID).Scan(&slots); err != nil {
		t.Fatalf("load consecutive slots: %v", err)
	}

	return slots
}
