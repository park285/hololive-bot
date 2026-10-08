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

// unresolvableFixture는 시작을 관측한 LIVE 세션과 영상 확인 lease, 해소 불가 grace 10분의 consumer다.
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

// checkVideo는 영상 확인 lease를 delta만큼 진행한 뒤 주어진 사유의 확인을 게시·소비하고 결정을 단언한다.
func (f *unresolvableFixture) checkVideo(t *testing.T, delta time.Duration, reason contract.LiveCheckUnknownReason, decision string) {
	t.Helper()

	if delta > 0 {
		f.videoProof = advanceLease(t.Context(), t, f.pool, &f.videoProof, delta)
	}

	id := publishLiveCheck(t.Context(), t, f.publisher, videoLiveCheckEnvelope(t, &f.videoProof, identityMissingVideoCheck(reason)))
	consumeLiveChecks(t.Context(), t, f.consumer)
	assertApplicationDecision(t, f.pool, id, liveSessionEntityKind, decision)
}

// channelNegative는 현재 영상 확인 예정 시각 기준의 identity 확인 채널 페이지 음성을 저장한다.
func (f *unresolvableFixture) channelNegative(t *testing.T) {
	t.Helper()

	f.channelCheck(t, contract.ChannelLiveCheckV1{Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true})
}

// channelCheck는 현재 영상 확인 예정 시각 기준의 채널 /live 확인 최신값을 저장한다.
func (f *unresolvableFixture) channelCheck(t *testing.T, payload contract.ChannelLiveCheckV1) {
	t.Helper()

	checkProof := seedChannelLiveCheckLease(t, f.pool, &f.videoProof)
	publishLiveCheck(t.Context(), t, f.publisher, channelLiveCheckEnvelope(t, &checkProof, payload))
	consumeLiveChecks(t.Context(), t, f.consumer)
}

// 시작을 관측한 LIVE가 비공개·삭제로 바뀌어 identity_missing만 이어지면 첫 관측 시각을 head에 두고, 지속 시간 안이거나
// 채널 음성이 영상 확인 시각 기준 5분 밖이면 LIVE로 남긴다.
func TestVideoLifecycleUnresolvableVideoRetainsInsideGraceOrWithoutFreshChannelNegative(t *testing.T) {
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

// 지속 시간이 지나고 같은 채널의 신선한 /live 음성이 있으면 UNRESOLVABLE_VIDEO로 끝낸다. 종료 시각은 첫 identity_missing
// 시각이며 시작·positive clock·알림은 그대로다.
func TestVideoLifecycleUnresolvableVideoEndsAfterSustainedIdentityMissingAndChannelNegative(t *testing.T) {
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

// 다른 UNKNOWN 사유는 기존처럼 수명을 바꾸지 않고, 추적 중 positive가 오면 추적을 지운다.
func TestVideoLifecycleUnresolvableVideoRequiresIdentityMissingAndClearsOnPositive(t *testing.T) {
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

// startUnresolvableUpcoming은 지난 일정의 UPCOMING 세션과 영상 확인 lease, 해소 불가 grace 0의 consumer다.
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

// UPCOMING의 identity_missing은 추적만 하고, grace가 지나고 채널 음성이 있어도 끝내지 않는다. 이후 positive는 추적을 지운다.
func TestVideoLifecycleUnresolvableUpcomingTracksWithoutEnding(t *testing.T) {
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

// UPCOMING의 채널 불일치 확인도 추적하며, 다른 UNKNOWN 사유는 추적하지 않는다.
func TestVideoLifecycleUnresolvableUpcomingTracksIdentityMismatchOnly(t *testing.T) {
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

// 채널 /live 최신값이 신선해도 identity 미확인 UNKNOWN이나 다른 영상의 LIVE면 음성이 아니므로 끝내지 않는다.
// 로봇 확인이 /live를 함께 막거나 채널이 다른 영상으로 방송 중인 경우다.
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
// 채널 음성과 grace가 갖춰져도 끝내지 않고 이번 관측부터 다시 추적한다.
func TestVideoLifecycleUnresolvableVideoRestartsStaleTracking(t *testing.T) {
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

// head 없는 metadata_only UPCOMING은 미확정 메타데이터가 head의 근거가 아니므로 추적하지 않고 기존 결정을 남긴다.
func TestVideoLifecycleHeadlessMetadataUpcomingKeepsIdentityDecision(t *testing.T) {
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
