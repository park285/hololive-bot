package sourceobservation

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

// identityMissingVideoCheck는 비공개·삭제 영상의 익명 player 응답처럼 identity 없이 UNKNOWN만 남기는 확인이다.
func identityMissingVideoCheck(reason contract.LiveCheckUnknownReason) contract.VideoLiveCheckV1 {
	return contract.VideoLiveCheckV1{
		VideoID: testVideoID, Availability: contract.VideoAvailabilityUnknown, Method: contract.VideoAvailabilityMethodUnknown,
		UnknownReason: reason, Coverage: contract.VideoLiveCheckCoverageV1{VideoID: testVideoID},
	}
}

func loadUnresolvableSince(t *testing.T, pool *pgxpool.Pool) *time.Time {
	t.Helper()

	var since *time.Time

	if err := pool.QueryRow(t.Context(), `SELECT unresolvable_since FROM youtube_live_reconciliation_heads WHERE video_id=$1`, testVideoID).Scan(&since); err != nil {
		t.Fatalf("load unresolvable_since: %v", err)
	}

	return since
}

type unresolvableFixture struct {
	pool       *pgxpool.Pool
	publisher  *publishkit.Publisher
	consumer   *Consumer
	proof      contract.LeaseProof
	videoProof contract.LeaseProof
	startedAt  time.Time
}

func startUnresolvable(t *testing.T) *unresolvableFixture {
	t.Helper()

	pool, _, consumer, proof := startLivePersist(t)
	publisher := publishkit.NewPublisher(pool)
	positive := liveSession(testVideoID, testStatusLive)

	consumer.WithLiveUnresolvableGrace(10 * time.Minute)

	positive.StartedAt = new(proof.ScheduledFor.Add(-time.Hour))
	proof = publishConsumeLive(t.Context(), t, pool, publisher, consumer, &proof, positive)

	return &unresolvableFixture{
		pool: pool, publisher: publisher, consumer: consumer, proof: proof, startedAt: *positive.StartedAt,
		videoProof: seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live"),
	}
}

func (f *unresolvableFixture) checkVideo(t *testing.T, delta time.Duration, reason contract.LiveCheckUnknownReason, decision string) {
	t.Helper()

	if delta > 0 {
		f.videoProof = advanceLease(t.Context(), t, f.pool, &f.videoProof, delta)
	}

	id := publishLiveCheck(t.Context(), t, f.publisher, videoLiveCheckEnvelope(t, &f.videoProof, identityMissingVideoCheck(reason)))
	consumeLiveChecks(t.Context(), t, f.consumer)
	assertApplicationDecision(t, f.pool, id, liveSessionEntityKind, decision)
}

func (f *unresolvableFixture) channelNegative(t *testing.T) {
	t.Helper()

	f.channelCheck(t, contract.ChannelLiveCheckV1{Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true})
}

func (f *unresolvableFixture) channelCheck(t *testing.T, payload contract.ChannelLiveCheckV1) {
	t.Helper()

	checkProof := seedChannelLiveCheckLease(t, f.pool, &f.videoProof)
	publishLiveCheck(t.Context(), t, f.publisher, channelLiveCheckEnvelope(t, &checkProof, payload))
	consumeLiveChecks(t.Context(), t, f.consumer)
}

func TestVideoLifecycleUnresolvableVideoRetainsInsideGraceOrWithoutFreshChannelNegative(t *testing.T) {
	t.Parallel()

	f := startUnresolvable(t)
	firstAt := f.videoProof.ScheduledFor

	f.checkVideo(t, 0, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_TRACKED")

	if since := loadUnresolvableSince(t, f.pool); since == nil || !since.Equal(firstAt) || liveSessionStatus(t, f.pool) != testStatusLive {
		t.Fatalf("first identity_missing: since=%v status=%s, want LIVE tracked since %s", since, liveSessionStatus(t, f.pool), firstAt)
	}

	f.channelNegative(t)
	f.checkVideo(t, time.Minute, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_RETAINED")
	f.checkVideo(t, 10*time.Minute, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_RETAINED")

	if since := loadUnresolvableSince(t, f.pool); since == nil || !since.Equal(firstAt) || liveSessionStatus(t, f.pool) != testStatusLive {
		t.Fatalf("retained identity_missing: since=%v status=%s, want LIVE tracked since %s", since, liveSessionStatus(t, f.pool), firstAt)
	}
}

func TestVideoLifecycleUnresolvableVideoEndsAfterSustainedIdentityMissingAndChannelNegative(t *testing.T) {
	t.Parallel()

	f := startUnresolvable(t)
	firstAt := f.videoProof.ScheduledFor

	f.checkVideo(t, 0, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_TRACKED")
	f.checkVideo(t, 10*time.Minute, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_RETAINED")
	f.channelNegative(t)
	f.checkVideo(t, time.Minute, contract.LiveCheckReasonIdentityMissing, "ENDED")

	got := loadVideoLifecycle(t, f.pool)
	if got.status != testStatusEnded || got.headStatus != testStatusEnded || got.endReason != "UNRESOLVABLE_VIDEO" || !got.endedAt.Equal(firstAt) ||
		!got.livePositiveAt.Equal(f.proof.ScheduledFor.Add(-time.Minute)) || got.candidate != "" {
		t.Fatalf("sustained identity_missing with channel negative = %+v, want UNRESOLVABLE_VIDEO ended at %s", got, firstAt)
	}

	var started *time.Time

	if err := f.pool.QueryRow(t.Context(), `SELECT started_at FROM youtube_live_sessions WHERE video_id=$1`, testVideoID).Scan(&started); err != nil {
		t.Fatal(err)
	}

	if started == nil || !started.Equal(f.startedAt) {
		t.Fatalf("unresolvable end changed started_at: %v, want %s", started, f.startedAt)
	}

	assertTableCount(t, f.pool, "youtube_notification_outbox", 0)
	assertTableCount(t, f.pool, "youtube_live_pending_ends", 0)
}

func TestVideoLifecycleUnresolvableVideoRequiresIdentityMissingAndClearsOnPositive(t *testing.T) {
	t.Parallel()

	f := startUnresolvable(t)

	f.checkVideo(t, 0, contract.LiveCheckReasonRequestFailed, videoLifecycleIdentityUnverified)

	if since := loadUnresolvableSince(t, f.pool); since != nil {
		t.Fatalf("request_failed started unresolvable tracking at %s", since)
	}

	f.checkVideo(t, time.Minute, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_TRACKED")

	if since := loadUnresolvableSince(t, f.pool); since == nil {
		t.Fatal("identity_missing did not start unresolvable tracking")
	}

	f.proof = advanceLease(t.Context(), t, f.pool, &f.proof, 2*time.Minute)
	publishConsumeLive(t.Context(), t, f.pool, f.publisher, f.consumer, &f.proof, liveSession(testVideoID, testStatusLive))

	if since := loadUnresolvableSince(t, f.pool); since != nil || liveSessionStatus(t, f.pool) != testStatusLive {
		t.Fatalf("live positive kept unresolvable tracking: since=%v status=%s", since, liveSessionStatus(t, f.pool))
	}
}

func startUnresolvableUpcoming(t *testing.T) *unresolvableFixture {
	t.Helper()

	pool, _, consumer, proof := startLivePersist(t)
	publisher := publishkit.NewPublisher(pool)

	proof = publishConsumeLive(t.Context(), t, pool, publisher, consumer, &proof, liveSession(testVideoID, testStatusUpcoming))

	return &unresolvableFixture{
		pool: pool, publisher: publisher, consumer: consumer, proof: proof,
		videoProof: seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live"),
	}
}

func TestVideoLifecycleUnresolvableUpcomingTracksWithoutEnding(t *testing.T) {
	t.Parallel()

	f := startUnresolvableUpcoming(t)
	firstAt := f.videoProof.ScheduledFor

	f.checkVideo(t, 0, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_TRACKED")

	if since := loadUnresolvableSince(t, f.pool); since == nil || !since.Equal(firstAt) || liveSessionStatus(t, f.pool) != testStatusUpcoming {
		t.Fatalf("upcoming identity_missing: since=%v status=%s, want UPCOMING tracked since %s", since, liveSessionStatus(t, f.pool), firstAt)
	}

	f.videoProof = advanceLease(t.Context(), t, f.pool, &f.videoProof, 11*time.Minute)
	f.channelNegative(t)
	f.checkVideo(t, 0, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_RETAINED")

	if since := loadUnresolvableSince(t, f.pool); since == nil || !since.Equal(firstAt) || liveSessionStatus(t, f.pool) != testStatusUpcoming {
		t.Fatalf("upcoming identity_missing after grace: since=%v status=%s, want UPCOMING kept", since, liveSessionStatus(t, f.pool))
	}

	assertTableCount(t, f.pool, "youtube_live_pending_ends", 0)

	f.proof = advanceLease(t.Context(), t, f.pool, &f.proof, 12*time.Minute)
	publishConsumeLive(t.Context(), t, f.pool, f.publisher, f.consumer, &f.proof, liveSession(testVideoID, testStatusUpcoming))

	if since := loadUnresolvableSince(t, f.pool); since != nil || liveSessionStatus(t, f.pool) != testStatusUpcoming {
		t.Fatalf("upcoming positive kept unresolvable tracking: since=%v status=%s", since, liveSessionStatus(t, f.pool))
	}
}

func TestVideoLifecycleUnresolvableUpcomingTracksIdentityMismatchOnly(t *testing.T) {
	t.Parallel()

	f := startUnresolvableUpcoming(t)

	f.checkVideo(t, 0, contract.LiveCheckReasonRequestFailed, videoLifecycleIdentityUnverified)

	if since := loadUnresolvableSince(t, f.pool); since != nil {
		t.Fatalf("request_failed started upcoming tracking at %s", since)
	}

	f.videoProof = advanceLease(t.Context(), t, f.pool, &f.videoProof, time.Minute)

	mismatch := liveVideoCheck(contract.VideoAvailabilityPublic)

	mismatch.ChannelID = "UC_OTHER"

	id := publishLiveCheck(t.Context(), t, f.publisher, videoLiveCheckEnvelope(t, &f.videoProof, mismatch))
	consumeLiveChecks(t.Context(), t, f.consumer)
	assertApplicationDecision(t, f.pool, id, liveSessionEntityKind, "UNRESOLVABLE_TRACKED")

	if since := loadUnresolvableSince(t, f.pool); since == nil || !since.Equal(f.videoProof.ScheduledFor) || liveSessionStatus(t, f.pool) != testStatusUpcoming {
		t.Fatalf("identity_mismatch on upcoming: since=%v status=%s, want tracked UPCOMING", since, liveSessionStatus(t, f.pool))
	}

	if got := loadVideoAvailability(t, f.pool); !sameReason(got.unknownReason, contract.LiveCheckReasonIdentityMismatch) {
		t.Fatalf("availability = %+v, want identity_mismatch", got)
	}
}

func TestVideoLifecycleUnresolvableVideoRequiresConfirmedChannelNegative(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload contract.ChannelLiveCheckV1
	}{
		{name: "unknown unconfirmed", payload: contract.ChannelLiveCheckV1{
			Outcome: contract.ChannelLiveCheckUnknown, UnknownReason: contract.LiveCheckReasonRequestFailed,
		}},
		{name: "other live video", payload: contract.ChannelLiveCheckV1{
			Outcome: contract.ChannelLiveCheckLiveVideo, SelectedVideoID: "vid-other-live", ChannelIdentityConfirmed: true,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := startUnresolvable(t)

			f.checkVideo(t, 0, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_TRACKED")
			f.checkVideo(t, 10*time.Minute, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_RETAINED")
			f.channelCheck(t, tc.payload)
			f.checkVideo(t, time.Minute, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_RETAINED")

			if status := liveSessionStatus(t, f.pool); status != testStatusLive {
				t.Fatalf("%s channel check ended session: status=%s", tc.name, status)
			}
		})
	}
}

// 7.2.7 롤백 창처럼 positive가 지우지 못한 잔여 추적값(마지막 positive보다 이른 값)은 지속 시간의 근거가 아니다.
func TestVideoLifecycleUnresolvableVideoRestartsStaleTracking(t *testing.T) {
	t.Parallel()

	f := startUnresolvable(t)

	if _, err := f.pool.Exec(t.Context(), `UPDATE youtube_live_reconciliation_heads
		SET unresolvable_since = last_live_positive_at - INTERVAL '30 minutes' WHERE video_id=$1`, testVideoID); err != nil {
		t.Fatal(err)
	}

	f.videoProof = advanceLease(t.Context(), t, f.pool, &f.videoProof, 11*time.Minute)

	f.channelNegative(t)
	f.checkVideo(t, 0, contract.LiveCheckReasonIdentityMissing, "UNRESOLVABLE_TRACKED")

	if since := loadUnresolvableSince(t, f.pool); since == nil || !since.Equal(f.videoProof.ScheduledFor) || liveSessionStatus(t, f.pool) != testStatusLive {
		t.Fatalf("stale tracking: since=%v status=%s, want LIVE tracked again since %s", since, liveSessionStatus(t, f.pool), f.videoProof.ScheduledFor)
	}
}

func TestVideoLifecycleHeadlessMetadataUpcomingKeepsIdentityDecision(t *testing.T) {
	t.Parallel()

	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,lifecycle_origin,scheduled_start_time)
		VALUES ($1,$2,'UPCOMING','','metadata_only',$3)`, testVideoID, testChannelID, proof.ScheduledFor.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	videoProof := seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")
	id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, identityMissingVideoCheck(contract.LiveCheckReasonIdentityMissing)))

	consumeLiveChecks(ctx, t, consumer)
	assertApplicationDecision(t, pool, id, liveSessionEntityKind, videoLifecycleIdentityUnverified)
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
}
