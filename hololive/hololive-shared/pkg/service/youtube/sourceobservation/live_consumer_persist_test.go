package sourceobservation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

func TestLiveConsumerUpcomingLiveEndedPersistsOnce(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "UPCOMING"))
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
	publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "ENDED"))
	if status := liveSessionStatus(t, pool); status != string(domain.LiveStatusEnded) {
		t.Fatalf("status = %s, want ENDED", status)
	}
	assertTableCount(t, pool, "youtube_notification_outbox", 0)
	assertTableCount(t, pool, "youtube_live_sessions", 1)
}

func TestLiveConsumerDoesNotRewriteUntouchedSession(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	lastSeen := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, last_seen_at)
		VALUES ('vid-keep', 'UC_TEST', 'LIVE', 'Keep', $1)
	`, lastSeen); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	repo := NewRepository(pool)
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindLiveSnapshot, "UC_TEST", "youtubejs_channel_live")
	consumer := newLiveTestConsumer(pool, repo, 0)
	if _, err := repo.PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, &proof, liveSession("vid-new", "LIVE")))); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
	}
	var title string
	var seen time.Time
	if err := pool.QueryRow(ctx, `SELECT title, last_seen_at FROM youtube_live_sessions WHERE video_id = 'vid-keep'`).Scan(&title, &seen); err != nil {
		t.Fatal(err)
	}
	if title != "Keep" || !seen.Equal(lastSeen) {
		t.Fatalf("untouched session rewritten: %s %s", title, seen)
	}
}

func TestLiveConsumerStaleEpochCannotCreateEndEvidence(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
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
	ctx := context.Background()
	envelope := liveSnapshotEnvelope(t, &proof, liveSession("vid-a", "LIVE"))
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	envelope.SourceEventAt = &future
	prepared, err := contract.PrepareEnvelope(*envelope)
	if err != nil {
		t.Fatalf("prepare skewed envelope: %v", err)
	}
	if _, err := repo.PublishBatch(ctx, publishInput(&prepared)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume: %v", err)
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
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof)
	publishConsumeLive(t, ctx, pool, repo, consumer, &proof)
	if liveSessionStatus(t, pool) != string(domain.LiveStatusEnded) {
		t.Fatal("two distinct scoped absence slots after grace must end")
	}
}

func TestLiveConsumerOneScopedAbsenceDoesNotEnd(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
	publishConsumeLive(t, ctx, pool, repo, consumer, &proof)
	if liveSessionStatus(t, pool) != string(domain.LiveStatusLive) {
		t.Fatal("one scoped absence must not end")
	}
	if slots := liveConsecutiveSlots(t, pool, "vid-a"); slots != 1 {
		t.Fatalf("consecutive = %d, want 1", slots)
	}
}

func TestLiveConsumerNeverLiveUpcomingScopedAbsenceDoesNotEnd(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "UPCOMING"))
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof)
	publishConsumeLive(t, ctx, pool, repo, consumer, &proof)
	if liveSessionStatus(t, pool) != string(domain.LiveStatusUpcoming) {
		t.Fatal("scoped absence must not end never-live UPCOMING")
	}
}

func TestLiveConsumerAlreadyEndedLateLiveStaysEnded(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "ENDED"))
	seen := liveLastSeen(t, pool)
	publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
	if liveSessionStatus(t, pool) != string(domain.LiveStatusEnded) {
		t.Fatal("already ENDED session must stay ENDED")
	}
	if !liveLastSeen(t, pool).Equal(seen) {
		t.Fatal("late LIVE must not rewrite an ENDED session")
	}
}

func TestLiveConsumerDoesNotStubOverwriteExistingLiveHead(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
	var liveAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT last_live_positive_at FROM youtube_live_reconciliation_heads WHERE video_id = 'vid-a'
	`).Scan(&liveAt); err != nil {
		t.Fatal(err)
	}
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "ENDED"))
	publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-new", "LIVE"))
	var status string
	var kept time.Time
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
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
	publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "ENDED"))
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
	if err := pool.QueryRow(ctx, `
		SELECT next_end_check_at FROM youtube_live_reconciliation_heads WHERE video_id = 'vid-a'
	`).Scan(&next); err != nil {
		t.Fatal(err)
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

func TestLiveEndFinalizerEndsAfterGraceWithoutNewObservation(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := context.Background()
	proof = publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "LIVE"))
	publishConsumeLive(t, ctx, pool, repo, consumer, &proof, liveSession("vid-a", "ENDED"))
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
	proof := seedPublishLease(t, context.Background(), pool, contract.ProviderYouTubeJS, contract.KindLiveSnapshot, "UC_TEST", "youtubejs_channel_live")
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
	return contract.LiveSessionV1{VideoID: videoID, ChannelID: "UC_TEST", Status: status}
}

func liveSnapshotEnvelope(t *testing.T, proof *contract.LeaseProof, sessions ...contract.LiveSessionV1) *contract.Envelope {
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
		statuses = []string{"LIVE", "UPCOMING", "ENDED", "CANCELLED"}
	}
	payload, err := contract.MarshalPayloadV1(contract.LiveSnapshotV1{
		Sessions: sessions,
		Coverage: contract.GlobalChannelCoverageV1{
			RequestedChannelIDs: []string{"UC_TEST"},
			Filters:             contract.LiveFiltersV1{Statuses: statuses},
		},
	})
	if err != nil {
		t.Fatalf("marshal live payload: %v", err)
	}
	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindLiveSnapshot, SubjectKey: "UC_TEST",
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: 1,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: payload, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare live envelope: %v", err)
	}
	return &envelope
}

func publishConsumeLive(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *Repository,
	consumer *Consumer,
	proof *contract.LeaseProof,
	sessions ...contract.LiveSessionV1,
) contract.LeaseProof {
	t.Helper()
	if _, err := repo.PublishBatch(ctx, publishInput(liveSnapshotEnvelope(t, proof, sessions...))); err != nil {
		t.Fatalf("publish live: %v", err)
	}
	if err := consumer.Consume(ctx, liveClaimOptions()); err != nil {
		t.Fatalf("consume live: %v", err)
	}
	return advanceLease(t, ctx, pool, proof, time.Minute)
}

func liveSessionStatus(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM youtube_live_sessions WHERE video_id = 'vid-a'`).Scan(&status); err != nil {
		t.Fatalf("load live status: %v", err)
	}
	return status
}

func liveLastSeen(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var seen time.Time
	if err := pool.QueryRow(context.Background(), `SELECT last_seen_at FROM youtube_live_sessions WHERE video_id = 'vid-a'`).Scan(&seen); err != nil {
		t.Fatalf("load last_seen_at: %v", err)
	}
	return seen
}

func liveConsecutiveSlots(t *testing.T, pool *pgxpool.Pool, videoID string) int {
	t.Helper()
	var slots int
	if err := pool.QueryRow(context.Background(), `
		SELECT consecutive_absence_slots FROM youtube_live_reconciliation_heads WHERE video_id = $1
	`, videoID).Scan(&slots); err != nil {
		t.Fatalf("load consecutive slots: %v", err)
	}
	return slots
}
