package sourceobservation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

// 채널 /live 확인은 최신값만 남기고 live reducer·pending·absence slot을 건드리지 않는다.
func TestChannelLiveCheckStoresLatestWithoutLiveLifecycle(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))

	checkProof := seedChannelLiveCheckLease(t, pool, &proof)

	before := loadVideoLifecycle(t, pool)
	id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), channelLiveCheckEnvelope(t, &checkProof, contract.ChannelLiveCheckV1{
		Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true,
	}))
	consumeLiveChecks(ctx, t, consumer)

	got := loadChannelLiveCheck(t, pool)
	if got.outcome != string(contract.ChannelLiveCheckChannelPage) || got.observationID == nil || *got.observationID != id ||
		!got.effectiveAt.Equal(checkProof.ScheduledFor) {
		t.Fatalf("channel live check = %+v, want CHANNEL_PAGE observation %d at %s", got, id, checkProof.ScheduledFor)
	}

	if after := loadVideoLifecycle(t, pool); after != before {
		t.Fatalf("channel live check changed live lifecycle: %+v -> %+v", before, after)
	}

	assertTableCount(t, pool, "youtube_live_pending_ends", 0)
	// 앞선 COMPLETE live_snapshot의 slot 하나만 남고 채널 확인은 slot을 더하지 않는다.
	assertTableCount(t, pool, "youtube_live_absence_slots", 1)
	assertApplicationDecision(t, pool, id, channelLiveCheckEntityKind, liveCheckDecisionApplied)
}

func TestChannelLiveCheckOlderObservationKeepsNewerUnknown(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = seedChannelLiveCheckLease(t, pool, &proof)

	newer := advanceLease(ctx, t, pool, &proof, 2*time.Minute)
	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), channelLiveCheckEnvelope(t, &newer, contract.ChannelLiveCheckV1{
		Outcome: contract.ChannelLiveCheckUnknown, UnknownReason: contract.LiveCheckReasonRequestFailed,
	}))
	consumeLiveChecks(ctx, t, consumer)

	older := advanceLease(ctx, t, pool, &newer, -time.Minute)
	olderID := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), channelLiveCheckEnvelope(t, &older, contract.ChannelLiveCheckV1{
		Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true,
	}))
	consumeLiveChecks(ctx, t, consumer)

	got := loadChannelLiveCheck(t, pool)
	if got.outcome != string(contract.ChannelLiveCheckUnknown) || got.unknownReason == nil ||
		*got.unknownReason != string(contract.LiveCheckReasonRequestFailed) || !got.effectiveAt.Equal(newer.ScheduledFor) {
		t.Fatalf("older observation replaced newer UNKNOWN: %+v", got)
	}

	assertApplicationDecision(t, pool, olderID, channelLiveCheckEntityKind, liveCheckDecisionNotNewer)
}

// 같은 effective_at에서는 observation_id가 큰 관측이 이기고, retention으로 ID가 지워진 최신값은
// 같은 시각의 관측이 순서를 증명할 수 없으므로 유지된다.
func TestChannelLiveCheckSameEffectiveTimeOrdering(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = seedChannelLiveCheckLease(t, pool, &proof)
	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), channelLiveCheckEnvelope(t, &proof, contract.ChannelLiveCheckV1{
		Outcome: contract.ChannelLiveCheckChannelPage, ChannelIdentityConfirmed: true,
	}))
	consumeLiveChecks(ctx, t, consumer)

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	higherID := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), channelLiveCheckEnvelope(t, &proof, contract.ChannelLiveCheckV1{
		Outcome: contract.ChannelLiveCheckUpcomingVideo, SelectedVideoID: "vid-upcoming", ChannelIdentityConfirmed: true,
	}))
	moveChannelLiveCheckClock(t, pool, proof.ScheduledFor, false)
	consumeLiveChecks(ctx, t, consumer)

	got := loadChannelLiveCheck(t, pool)
	if got.outcome != string(contract.ChannelLiveCheckUpcomingVideo) || got.observationID == nil || *got.observationID != higherID {
		t.Fatalf("same-time higher observation did not win: %+v", got)
	}

	proof = advanceLease(ctx, t, pool, &proof, time.Minute)

	retainedID := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), channelLiveCheckEnvelope(t, &proof, contract.ChannelLiveCheckV1{
		Outcome: contract.ChannelLiveCheckUnknown, UnknownReason: contract.LiveCheckReasonStructureUnrecognized,
	}))
	moveChannelLiveCheckClock(t, pool, proof.ScheduledFor, true)
	consumeLiveChecks(ctx, t, consumer)

	got = loadChannelLiveCheck(t, pool)
	if got.outcome != string(contract.ChannelLiveCheckUpcomingVideo) || got.observationID != nil {
		t.Fatalf("same-time observation replaced retention-cleared latest: %+v", got)
	}

	assertApplicationDecision(t, pool, retainedID, channelLiveCheckEntityKind, liveCheckDecisionNotNewer)
}

// 검증된 upstream 종료 시각만 추적 LIVE를 끝내며 ended_at은 관측 슬롯이 아니라 그 시각이다.
func TestVideoLiveCheckVerifiedEndUsesUpstreamEndedAt(t *testing.T) {
	for _, missingStart := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing_start=%t", missingStart), func(t *testing.T) {
			pool, consumer, liveProof, videoProof := startVideoLiveCheck(t)
			ctx := t.Context()
			endedAt := liveProof.ScheduledFor.Add(30 * time.Second)

			if missingStart {
				if _, err := pool.Exec(ctx, `UPDATE youtube_live_sessions SET started_at=NULL WHERE video_id=$1`, testVideoID); err != nil {
					t.Fatal(err)
				}
			}

			id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, endedVideoCheck(endedAt)))
			consumeLiveChecks(ctx, t, consumer)

			got := loadVideoLifecycle(t, pool)
			if got.status != testStatusEnded || got.headStatus != testStatusEnded || !got.endedAt.Equal(endedAt) {
				t.Fatalf("lifecycle = %+v, want ENDED at upstream %s (slot %s)", got, endedAt, videoProof.ScheduledFor)
			}

			if got.endReason != string(live.EndReasonExplicitEnd) {
				t.Fatalf("end reason = %q, want %s", got.endReason, live.EndReasonExplicitEnd)
			}

			availability := loadVideoAvailability(t, pool)
			if availability.availability != string(contract.VideoAvailabilityPublic) || availability.observationID == nil ||
				*availability.observationID != id || !availability.identityConfirmed || availability.channelID != testChannelID {
				t.Fatalf("availability = %+v", availability)
			}

			assertTableCount(t, pool, "youtube_live_absence_slots", 1)
			assertTableCount(t, pool, "youtube_live_pending_ends", 0)
		})
	}
}

func TestVideoLiveCheckCurrentLiveRefreshesPositive(t *testing.T) {
	pool, consumer, _, videoProof := startVideoLiveCheck(t)
	ctx := t.Context()

	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, liveVideoCheck(contract.VideoAvailabilityMembersOnly)))
	consumeLiveChecks(ctx, t, consumer)

	got := loadVideoLifecycle(t, pool)
	if got.status != testStatusLive || !got.livePositiveAt.Equal(videoProof.ScheduledFor) {
		t.Fatalf("current LIVE did not refresh positive: %+v", got)
	}

	assertTableCount(t, pool, "youtube_live_absence_slots", 1)
}

var untrustedVideoCheckCases = []struct {
	name         string
	payload      func(*contract.LeaseProof) contract.VideoLiveCheckV1
	availability contract.VideoAvailability
	reason       contract.LiveCheckUnknownReason
	identity     bool
	decision     string
}{
	{
		name: "canonical channel mismatch",
		payload: func(proof *contract.LeaseProof) contract.VideoLiveCheckV1 {
			payload := endedVideoCheck(proof.ScheduledFor.Add(-30 * time.Second))

			payload.ChannelID = "UC_OTHER"

			return payload
		},
		availability: contract.VideoAvailabilityUnknown, reason: contract.LiveCheckReasonIdentityMismatch,
		decision: videoLifecycleIdentityMismatch,
	},
	{
		name: "identity missing",
		payload: func(*contract.LeaseProof) contract.VideoLiveCheckV1 {
			return unknownVideoCheck(contract.LiveCheckReasonIdentityMissing)
		},
		availability: contract.VideoAvailabilityUnknown, reason: contract.LiveCheckReasonIdentityMissing,
		decision: videoLifecycleIdentityUnverified,
	},
	{
		name: "private live fact alone",
		payload: func(*contract.LeaseProof) contract.VideoLiveCheckV1 {
			return liveVideoCheck(contract.VideoAvailabilityPublicUnavailable)
		},
		availability: contract.VideoAvailabilityPublicUnavailable, identity: true,
		decision: videoLifecycleUnavailableOnly,
	},
	{
		name: "unknown with end facts",
		payload: func(proof *contract.LeaseProof) contract.VideoLiveCheckV1 {
			payload := endedVideoCheck(proof.ScheduledFor.Add(-30 * time.Second))

			payload.Availability, payload.Method = contract.VideoAvailabilityUnknown, contract.VideoAvailabilityMethodUnknown
			payload.UnknownReason = contract.LiveCheckReasonContradictoryFields

			return payload
		},
		availability: contract.VideoAvailabilityUnknown, reason: contract.LiveCheckReasonContradictoryFields,
		identity: true, decision: videoLifecycleUntrusted,
	},
	{
		name: "not live without end timestamp",
		payload: func(proof *contract.LeaseProof) contract.VideoLiveCheckV1 {
			payload := endedVideoCheck(proof.ScheduledFor.Add(-30 * time.Second))

			payload.EndedAt = nil

			return payload
		},
		availability: contract.VideoAvailabilityPublic, identity: true, decision: videoLifecycleNoFact,
	},
}

func TestVideoLiveCheckWithoutTrustedLifecycleKeepsLive(t *testing.T) {
	for _, tc := range untrustedVideoCheckCases {
		t.Run(tc.name, func(t *testing.T) {
			pool, consumer, _, videoProof := startVideoLiveCheck(t)
			ctx := t.Context()
			before := loadVideoLifecycle(t, pool)

			id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, tc.payload(&videoProof)))
			consumeLiveChecks(ctx, t, consumer)

			if after := loadVideoLifecycle(t, pool); after != before {
				t.Fatalf("lifecycle changed: %+v -> %+v", before, after)
			}

			got := loadVideoAvailability(t, pool)
			if got.availability != string(tc.availability) || got.identityConfirmed != tc.identity ||
				got.channelID != testChannelID || !sameReason(got.unknownReason, tc.reason) {
				t.Fatalf("availability = %+v, want %s reason=%q identity=%t", got, tc.availability, tc.reason, tc.identity)
			}

			assertTableCount(t, pool, "youtube_live_pending_ends", 0)
			assertApplicationDecision(t, pool, id, liveSessionEntityKind, tc.decision)
		})
	}
}

// 재확인 실패는 이전 공개 불가 판정을 계속 쓰지 않도록 최신 UNKNOWN으로 대체한다.
func TestVideoLiveCheckRequestFailureReplacesUnavailable(t *testing.T) {
	pool, consumer, _, videoProof := startVideoLiveCheck(t)
	ctx := t.Context()

	private := endedVideoCheck(videoProof.ScheduledFor)

	private.EndedAt, private.Availability, private.Method = nil, contract.VideoAvailabilityPublicUnavailable, contract.VideoAvailabilityMethodPlayerPrivate
	private.IsPrivate = new(true)

	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, private))
	consumeLiveChecks(ctx, t, consumer)

	if got := loadVideoAvailability(t, pool); got.availability != string(contract.VideoAvailabilityPublicUnavailable) {
		t.Fatalf("availability = %+v, want PUBLIC_UNAVAILABLE", got)
	}

	next := advanceLease(ctx, t, pool, &videoProof, time.Minute)
	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &next, unknownVideoCheck(contract.LiveCheckReasonRequestFailed)))
	consumeLiveChecks(ctx, t, consumer)

	got := loadVideoAvailability(t, pool)
	if got.availability != string(contract.VideoAvailabilityUnknown) || got.identityConfirmed ||
		!sameReason(got.unknownReason, contract.LiveCheckReasonRequestFailed) || !got.effectiveAt.Equal(next.ScheduledFor) {
		t.Fatalf("request failure did not replace PUBLIC_UNAVAILABLE: %+v", got)
	}

	if status := liveSessionStatus(t, pool); status != testStatusLive {
		t.Fatalf("status = %s, want LIVE", status)
	}
}

// 영상 확인은 grace 안의 기존 pending을 지우지 않고, 검증된 종료만 같은 pending·finalizer 규칙을 따른다.
func TestVideoLiveCheckPreservesPendingPolicy(t *testing.T) {
	pool, repo, consumer, proof := startLivePersistGrace(t, time.Hour)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusEnded))

	videoProof := seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")
	before := loadPendingEnd(t, pool)

	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, unknownVideoCheck(contract.LiveCheckReasonStructureUnrecognized)))
	consumeLiveChecks(ctx, t, consumer)

	if after := loadPendingEnd(t, pool); after != before {
		t.Fatalf("UNKNOWN video check changed pending end: %+v -> %+v", before, after)
	}

	endedAt := proof.ScheduledFor.Add(-90 * time.Second)

	videoProof = advanceLease(ctx, t, pool, &videoProof, time.Minute)
	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, endedVideoCheck(endedAt)))
	consumeLiveChecks(ctx, t, consumer)

	if status := liveSessionStatus(t, pool); status != testStatusLive {
		t.Fatalf("verified end inside grace ended session: %s", status)
	}

	pending := loadPendingEnd(t, pool)
	if !pending.endedAt.Equal(endedAt) {
		t.Fatalf("pending end = %+v, want upstream ended_at %s", pending, endedAt)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE youtube_live_reconciliation_heads
		SET last_live_positive_seen_at = NOW() - INTERVAL '2 hours', next_end_check_at = NOW()
		WHERE video_id = $1
	`, testVideoID); err != nil {
		t.Fatalf("backdate grace: %v", err)
	}

	if processed, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour); err != nil || !processed {
		t.Fatalf("finalize due: processed=%t err=%v", processed, err)
	}

	got := loadVideoLifecycle(t, pool)
	if got.status != testStatusEnded || !got.endedAt.Equal(endedAt) {
		t.Fatalf("finalizer lifecycle = %+v, want ENDED at %s", got, endedAt)
	}
}

func TestVideoLiveCheckKeepsEndedSession(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()

	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusLive))
	proof = publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession(testVideoID, testStatusEnded))

	videoProof := seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")
	before := loadVideoLifecycle(t, pool)

	id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, liveVideoCheck(contract.VideoAvailabilityPublic)))
	consumeLiveChecks(ctx, t, consumer)

	if after := loadVideoLifecycle(t, pool); after != before || after.status != testStatusEnded {
		t.Fatalf("video check resurrected ENDED session: %+v -> %+v", before, after)
	}

	assertApplicationDecision(t, pool, id, liveSessionEntityKind, videoLifecycleKeepEnded)
}

func TestVideoLiveCheckWithoutSessionCreatesNothing(t *testing.T) {
	pool, _, consumer, proof := startLivePersist(t)
	ctx := t.Context()
	videoProof := seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")

	id := publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, liveVideoCheck(contract.VideoAvailabilityPublic)))
	consumeLiveChecks(ctx, t, consumer)

	assertTableCount(t, pool, "youtube_live_sessions", 0)
	assertTableCount(t, pool, "youtube_live_reconciliation_heads", 0)
	assertTableCount(t, pool, "youtube_video_availability", 0)
	assertApplicationDecision(t, pool, id, videoAvailabilityEntityKind, liveCheckDecisionNoSession)
}

func startVideoLiveCheck(t *testing.T) (*pgxpool.Pool, *Consumer, contract.LeaseProof, contract.LeaseProof) {
	t.Helper()

	pool, _, consumer, liveProof := startLivePersistGrace(t, 0)
	started := liveProof

	liveProof = publishConsumeLive(t.Context(), t, pool, publishkit.NewPublisher(pool), consumer, &liveProof, liveSession(testVideoID, testStatusLive))

	videoProof := seedAdditionalLease(t, pool, &liveProof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")

	return pool, consumer, started, videoProof
}

// seedChannelLiveCheckLease는 같은 채널의 채널 확인 target과 별도 exact-subject job lease를 만든다.
func seedChannelLiveCheckLease(t *testing.T, pool *pgxpool.Pool, proof *contract.LeaseProof) contract.LeaseProof {
	t.Helper()

	return seedAdditionalLease(t, pool, proof, contract.KindChannelLiveCheck, testChannelID, "youtubejs_channel_live_check")
}

func channelLiveCheckEnvelope(t *testing.T, proof *contract.LeaseProof, payload contract.ChannelLiveCheckV1) *contract.Envelope {
	t.Helper()

	payload.ChannelID = testChannelID
	payload.Coverage = contract.ChannelLiveCheckCoverageV1{ChannelID: testChannelID}

	completeness := contract.CompletenessComplete

	if payload.Outcome == contract.ChannelLiveCheckUnknown {
		completeness = contract.CompletenessUnknown
	}

	return liveCheckEnvelope(t, proof, contract.KindChannelLiveCheck, testChannelID, completeness, payload)
}

func videoLiveCheckEnvelope(t *testing.T, proof *contract.LeaseProof, payload contract.VideoLiveCheckV1) *contract.Envelope {
	t.Helper()

	completeness := contract.CompletenessPartial

	if payload.Availability == contract.VideoAvailabilityUnknown {
		completeness = contract.CompletenessUnknown
	}

	return liveCheckEnvelope(t, proof, contract.KindVideoLiveCheck, testVideoID, completeness, payload)
}

func liveCheckEnvelope(
	t *testing.T,
	proof *contract.LeaseProof,
	kind contract.ObservationKind,
	subjectKey string,
	completeness contract.Completeness,
	payload any,
) *contract.Envelope {
	t.Helper()

	raw, err := contract.MarshalPayloadV1(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", kind, err)
	}

	envelope, err := contract.PrepareEnvelope(contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: kind, SubjectKey: subjectKey,
		SchemaVersion: contract.SchemaVersionV1, ContractGeneration: contract.LiveCheckContractGeneration,
		ScheduledFor: proof.ScheduledFor, ObservedAt: proof.ScheduledFor.Add(time.Second),
		Completeness: completeness, Continuity: contract.ContinuityNotApplicable,
		Payload: raw, CollectorInstance: proof.OwnerInstance, Lease: *proof,
	})
	if err != nil {
		t.Fatalf("prepare %s envelope: %v", kind, err)
	}

	return &envelope
}

func endedVideoCheck(endedAt time.Time) contract.VideoLiveCheckV1 {
	return contract.VideoLiveCheckV1{
		VideoID: testVideoID, ChannelID: testChannelID, IdentityConfirmed: true,
		IsLiveNow: new(false), IsLiveContent: new(true), IsPrivate: new(false),
		HasLiveBroadcastDetails: new(true), EndedAt: &endedAt,
		Availability: contract.VideoAvailabilityPublic, Method: contract.VideoAvailabilityMethodPlayerPublic,
		Coverage: contract.VideoLiveCheckCoverageV1{VideoID: testVideoID},
	}
}

func liveVideoCheck(availability contract.VideoAvailability) contract.VideoLiveCheckV1 {
	payload := contract.VideoLiveCheckV1{
		VideoID: testVideoID, ChannelID: testChannelID, IdentityConfirmed: true,
		IsLive: new(true), IsLiveNow: new(true), IsLiveContent: new(true),
		HasLiveBroadcastDetails: new(true), Availability: availability,
		Coverage: contract.VideoLiveCheckCoverageV1{VideoID: testVideoID},
	}

	switch availability {
	case contract.VideoAvailabilityPublic:
		payload.Method, payload.IsPrivate = contract.VideoAvailabilityMethodPlayerPublic, new(false)
	case contract.VideoAvailabilityMembersOnly:
		payload.Method = contract.VideoAvailabilityMethodPlayerMembersOnly
	case contract.VideoAvailabilityPublicUnavailable:
		payload.Method, payload.IsPrivate = contract.VideoAvailabilityMethodPlayerPrivate, new(true)
	case contract.VideoAvailabilityUnknown:
		payload.Method = contract.VideoAvailabilityMethodUnknown
	}

	return payload
}

func unknownVideoCheck(reason contract.LiveCheckUnknownReason) contract.VideoLiveCheckV1 {
	return contract.VideoLiveCheckV1{
		VideoID: testVideoID, Availability: contract.VideoAvailabilityUnknown,
		Method: contract.VideoAvailabilityMethodUnknown, UnknownReason: reason,
		Coverage: contract.VideoLiveCheckCoverageV1{VideoID: testVideoID},
	}
}

func publishLiveCheck(ctx context.Context, t *testing.T, publisher *publishkit.Publisher, envelope *contract.Envelope) int64 {
	t.Helper()

	published, err := publisher.PublishBatch(ctx, publishInput(envelope))
	if err != nil {
		t.Fatalf("publish %s: %v", envelope.ObservationKind, err)
	}

	return published.Results[0].ObservationID
}

func consumeLiveChecks(ctx context.Context, t *testing.T, consumer *Consumer) {
	t.Helper()

	if err := consumer.Consume(ctx, ClaimOptions{
		ConsumerName: "youtube-live-processor",
		LeaseOwner:   testAPILeaseOwner,
		Kinds: []contract.ObservationKind{
			contract.KindLiveSnapshot, contract.KindChannelLiveCheck, contract.KindVideoLiveCheck,
		},
		Limit:         10,
		LeaseDuration: 30 * time.Second,
	}); err != nil {
		t.Fatalf("consume live checks: %v", err)
	}
}

// 같은 시각 순서를 만들기 위해 저장된 최신값의 시각을 다음 관측 슬롯으로 옮긴다.
// 관측 ID 제거는 retention의 ON DELETE SET NULL 결과를 재현한다.
func moveChannelLiveCheckClock(t *testing.T, pool *pgxpool.Pool, at time.Time, clearObservation bool) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		UPDATE youtube_channel_live_checks
		SET scheduled_for = $2, effective_at = $2,
		    observation_id = CASE WHEN $3::boolean THEN NULL ELSE observation_id END
		WHERE channel_id = $1
	`, testChannelID, at, clearObservation); err != nil {
		t.Fatalf("move channel live check clock: %v", err)
	}
}

type channelLiveCheckRow struct {
	outcome       string
	unknownReason *string
	observationID *int64
	effectiveAt   time.Time
}

func loadChannelLiveCheck(t *testing.T, pool *pgxpool.Pool) channelLiveCheckRow {
	t.Helper()

	var row channelLiveCheckRow

	if err := pool.QueryRow(t.Context(), `
		SELECT outcome, unknown_reason, observation_id, effective_at
		FROM youtube_channel_live_checks WHERE channel_id = $1
	`, testChannelID).Scan(&row.outcome, &row.unknownReason, &row.observationID, &row.effectiveAt); err != nil {
		t.Fatalf("load channel live check: %v", err)
	}

	return row
}

type videoAvailabilityRow struct {
	channelID         string
	availability      string
	unknownReason     *string
	identityConfirmed bool
	observationID     *int64
	effectiveAt       time.Time
}

func loadVideoAvailability(t *testing.T, pool *pgxpool.Pool) videoAvailabilityRow {
	t.Helper()

	var row videoAvailabilityRow

	if err := pool.QueryRow(t.Context(), `
		SELECT channel_id, availability, unknown_reason, identity_confirmed, observation_id, effective_at
		FROM youtube_video_availability WHERE video_id = $1
	`, testVideoID).Scan(&row.channelID, &row.availability, &row.unknownReason, &row.identityConfirmed,
		&row.observationID, &row.effectiveAt); err != nil {
		t.Fatalf("load video availability: %v", err)
	}

	return row
}

// nil 시각은 zero, nil 사유는 빈 문자열로 담아 전후 동일성을 == 로 확인한다.
type videoLifecycleRow struct {
	status         string
	headStatus     string
	endedAt        time.Time
	endReason      string
	livePositiveAt time.Time
	candidate      string
	lastSeenAt     time.Time
}

func loadVideoLifecycle(t *testing.T, pool *pgxpool.Pool) videoLifecycleRow {
	t.Helper()

	var (
		row                     videoLifecycleRow
		endedAt, livePositiveAt *time.Time
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT session.status, head.status, session.ended_at, COALESCE(head.end_reason, ''),
		       head.last_live_positive_at, COALESCE(head.end_candidate_kind, ''), session.last_seen_at
		FROM youtube_live_sessions AS session
		JOIN youtube_live_reconciliation_heads AS head USING (video_id)
		WHERE session.video_id = $1
	`, testVideoID).Scan(&row.status, &row.headStatus, &endedAt, &row.endReason,
		&livePositiveAt, &row.candidate, &row.lastSeenAt); err != nil {
		t.Fatalf("load video lifecycle: %v", err)
	}

	row.endedAt = timeValue(endedAt)
	row.livePositiveAt = timeValue(livePositiveAt)
	row.lastSeenAt = row.lastSeenAt.UTC()

	return row
}

type pendingEndRow struct {
	kind          string
	observationID int64
	effectiveAt   time.Time
	endedAt       time.Time
}

func loadPendingEnd(t *testing.T, pool *pgxpool.Pool) pendingEndRow {
	t.Helper()

	var (
		row     pendingEndRow
		endedAt *time.Time
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT kind, observation_id, effective_at, ended_at
		FROM youtube_live_pending_ends WHERE video_id = $1
	`, testVideoID).Scan(&row.kind, &row.observationID, &row.effectiveAt, &endedAt); err != nil {
		t.Fatalf("load pending end: %v", err)
	}

	row.effectiveAt = row.effectiveAt.UTC()
	row.endedAt = timeValue(endedAt)

	return row
}

func assertApplicationDecision(t *testing.T, pool *pgxpool.Pool, observationID int64, entityKind, want string) {
	t.Helper()

	var decision string

	if err := pool.QueryRow(t.Context(), `
		SELECT decision FROM source_observation_applications
		WHERE observation_id = $1 AND entity_kind = $2
	`, observationID, entityKind).Scan(&decision); err != nil {
		t.Fatalf("load application %d/%s: %v", observationID, entityKind, err)
	}

	if decision != want {
		t.Fatalf("application %d/%s decision = %s, want %s", observationID, entityKind, decision, want)
	}
}

func sameReason(got *string, want contract.LiveCheckUnknownReason) bool {
	if want == "" {
		return got == nil
	}

	return got != nil && *got == string(want)
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}

	return value.UTC()
}

func TestVideoLiveCheckDoesNotSettleNewerEndWithoutTimestamp(t *testing.T) {
	pool, consumer, liveProof, videoProof := startVideoLiveCheck(t)
	ctx := t.Context()
	newer := videoProof.ScheduledFor.Add(time.Minute)

	if _, err := pool.Exec(ctx, `INSERT INTO youtube_live_pending_ends
		(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
		VALUES($1,$2,'EXPLICIT_END',900001,$3,$3,$3,true,true)`, testVideoID, testChannelID, newer); err != nil {
		t.Fatal(err)
	}

	endedAt := liveProof.ScheduledFor.Add(30 * time.Second)
	publishLiveCheck(ctx, t, publishkit.NewPublisher(pool), videoLiveCheckEnvelope(t, &videoProof, endedVideoCheck(endedAt)))
	consumeLiveChecks(ctx, t, consumer)

	got := loadVideoLifecycle(t, pool)
	if got.status != testStatusLive || got.headStatus != testStatusLive || !got.endedAt.IsZero() {
		t.Fatalf("older check settled unrelated pending: %+v", got)
	}

	pending := loadPendingEnd(t, pool)
	if !pending.effectiveAt.Equal(newer) || !pending.endedAt.IsZero() {
		t.Fatalf("newer retained evidence changed: %+v", pending)
	}
}
