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

	checkProof := seedChannelLiveCheckLease(t, f.pool, &f.videoProof)
	publishLiveCheck(t.Context(), t, f.publisher, channelLiveCheckEnvelope(t, &checkProof, contract.ChannelLiveCheckV1{
		Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true,
	}))
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
