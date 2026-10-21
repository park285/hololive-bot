package sourceobservation

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

const providerTimesSource = "provider"

// endedFixture는 종료 관측(detectedAt) 뒤 확정을 기다리는 ENDED 세션이다. 종료 사유(endReason)가 비면 head가 없다.
type endedFixture struct {
	origin     string
	endReason  string
	timesSrc   string
	startedAt  *time.Time
	endedAt    time.Time
	positiveAt *time.Time
	detectedAt time.Time
}

type finalizationHarness struct {
	pool     *pgxpool.Pool
	consumer *Consumer
	proof    contract.LeaseProof
}

// defaultEndedFixture는 S(첫 확인 예정 시각) 20분 전에 끝나 3분 전에 EXPLICIT_END 종료를 관측한 신 lifecycle 세션이다.
func defaultEndedFixture(slot time.Time) endedFixture {
	return endedFixture{
		origin: string(live.OriginObserved), endReason: string(live.EndReasonExplicitEnd), timesSrc: timesSourceObserved,
		startedAt: new(slot.Add(-2 * time.Hour)), endedAt: slot.Add(-20 * time.Minute),
		positiveAt: new(slot.Add(-25 * time.Minute)), detectedAt: slot.Add(-3 * time.Minute),
	}
}

func startFinalization(t *testing.T, edit func(*endedFixture)) *finalizationHarness {
	t.Helper()

	pool, _, consumer, proof := startLivePersistGrace(t, 0)
	fixture := defaultEndedFixture(proof.ScheduledFor)

	if edit != nil {
		edit(&fixture)
	}

	if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_sessions
        (video_id,channel_id,status,title,started_at,ended_at,live_first_seen_at,last_seen_at,lifecycle_origin,
         status_observed_at,times_source)
        VALUES ($1,$2,'ENDED','archive',$3,$4,$3,$5,$6,$5,$7)`,
		testVideoID, testChannelID, fixture.startedAt, fixture.endedAt, fixture.detectedAt, fixture.origin, fixture.timesSrc); err != nil {
		t.Fatal(err)
	}

	if fixture.endReason != "" {
		if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_reconciliation_heads
            (video_id,status,last_live_positive_at,last_live_positive_seen_at,ended_at,end_reason)
            VALUES ($1,'ENDED',$2,$2,$3,$4)`, testVideoID, fixture.positiveAt, fixture.endedAt, fixture.endReason); err != nil {
			t.Fatal(err)
		}
	}

	return &finalizationHarness{
		pool: pool, consumer: consumer,
		proof: seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live"),
	}
}

// observe는 현재 lease slot에서 영상 확인을 발행·정산하고 관측 ID를 돌려준다.
func (h *finalizationHarness) observe(t *testing.T, payload contract.VideoLiveCheckV1) int64 {
	t.Helper()

	id := publishLiveCheck(t.Context(), t, publishkit.NewPublisher(h.pool), videoLiveCheckEnvelope(t, &h.proof, payload))
	consumeLiveChecks(t.Context(), t, h.consumer)

	return id
}

// observeAt은 lease slot을 at - 1초로 옮긴 뒤 관측한다(관측 시각 = slot + 1초).
func (h *finalizationHarness) observeAt(t *testing.T, at time.Time, payload contract.VideoLiveCheckV1) int64 {
	t.Helper()

	h.proof = advanceLease(t.Context(), t, h.pool, &h.proof, at.Add(-time.Second).Sub(h.proof.ScheduledFor))

	return h.observe(t, payload)
}

func archiveVideoCheck(startedAt, endedAt time.Time) contract.VideoLiveCheckV1 {
	payload := endedVideoCheck(endedAt)

	payload.StartedAt = &startedAt

	return payload
}

type finalizationRow struct {
	timesSource       string
	startedAt         *time.Time
	endedAt           *time.Time
	headEndedAt       *time.Time
	historySortAt     time.Time
	outcome           *string
	reason            *string
	attempts          int16
	deferrals         int16
	nextAttemptAt     *time.Time
	completed         bool
	observedStartedAt *time.Time
	observedEndedAt   *time.Time
}

func loadFinalizationRow(t *testing.T, pool *pgxpool.Pool) finalizationRow {
	t.Helper()

	var row finalizationRow

	if err := pool.QueryRow(t.Context(), `SELECT session.times_source,session.started_at,session.ended_at,head.ended_at,
            session.history_sort_at,progress.outcome,progress.reason,COALESCE(progress.attempts,0),
            COALESCE(progress.deferrals,0),progress.next_attempt_at,progress.completed_at IS NOT NULL,
            progress.observed_started_at,progress.observed_ended_at
        FROM youtube_live_sessions session
        LEFT JOIN youtube_live_reconciliation_heads head USING(video_id)
        LEFT JOIN youtube_live_time_finalizations progress USING(video_id)
        WHERE session.video_id=$1`, testVideoID).Scan(
		&row.timesSource, &row.startedAt, &row.endedAt, &row.headEndedAt, &row.historySortAt, &row.outcome, &row.reason,
		&row.attempts, &row.deferrals, &row.nextAttemptAt, &row.completed, &row.observedStartedAt, &row.observedEndedAt,
	); err != nil {
		t.Fatal(err)
	}

	return row
}

func sameTime(got *time.Time, want time.Time) bool {
	return got != nil && got.Equal(want)
}

func valueOr(value *string) string {
	if value == nil {
		return "<nil>"
	}

	return *value
}

func TestTimeFinalizationReplacesObservedTimesWithArchiveTimes(t *testing.T) {
	t.Parallel()

	h := startFinalization(t, nil)
	slot := h.proof.ScheduledFor
	before := loadFinalizationRow(t, h.pool)
	start, end := slot.Add(-2*time.Hour-time.Minute), slot.Add(-22*time.Minute)

	id := h.observe(t, archiveVideoCheck(start, end))

	got := loadFinalizationRow(t, h.pool)
	if got.timesSource != providerTimesSource || !sameTime(got.startedAt, start) || !sameTime(got.endedAt, end) ||
		!sameTime(got.headEndedAt, end) || !got.historySortAt.Equal(end) {
		t.Fatalf("session times = %s %v~%v head=%v sort=%v; want provider %v~%v",
			got.timesSource, got.startedAt, got.endedAt, got.headEndedAt, got.historySortAt, start, end)
	}

	if valueOr(got.outcome) != finalizationFinalized || got.reason != nil || !got.completed ||
		!sameTime(got.observedStartedAt, *before.startedAt) || !sameTime(got.observedEndedAt, *before.endedAt) {
		t.Fatalf("progress = %s/%s completed=%v copies=%v~%v", valueOr(got.outcome), valueOr(got.reason),
			got.completed, got.observedStartedAt, got.observedEndedAt)
	}

	assertApplicationDecision(t, h.pool, id, liveSessionEntityKind, videoTimeFinalized)

	// 확정 뒤 같은 영상의 늦은 LIVE 판정도 ENDED 세션의 확정 시각을 관측 시각으로 되돌리지 않는다.
	if err := dbx.InPgxTx(t.Context(), h.pool, func(tx dbx.Tx) error {
		return persistLateLivePositive(t, tx, id+1, slot.Add(time.Hour))
	}); err != nil {
		t.Fatal(err)
	}

	after := loadFinalizationRow(t, h.pool)
	if after.timesSource != providerTimesSource || !sameTime(after.startedAt, start) || !sameTime(after.endedAt, end) {
		t.Fatalf("later upsert changed finalized times: %s %v~%v", after.timesSource, after.startedAt, after.endedAt)
	}
}

// persistLateLivePositive는 consumer처럼 세션을 잠그고 읽은 뒤 늦은 LIVE positive를 reducer로 판정해 저장한다.
func persistLateLivePositive(t *testing.T, tx dbx.Tx, observationID int64, at time.Time) error {
	t.Helper()

	state, err := loadLiveState(t.Context(), tx, nil, []string{testVideoID})
	if err != nil {
		return err
	}

	decision, err := live.Reduce(state, live.Evidence{
		Kind: contract.KindLiveSnapshot, ObservationID: observationID,
		ScheduledFor: at, EffectiveAt: at, ReceivedAt: at,
		Completeness: contract.CompletenessPartial, Continuity: contract.ContinuityNotApplicable,
		Sessions: []live.SessionFact{{
			VideoID: testVideoID, ChannelID: testChannelID, Status: testStatusLive,
			LiveStartConfirmed: true, StartedAt: &at,
		}},
	}, 0, at)
	if err != nil {
		return fmt.Errorf("reduce late live: %w", err)
	}

	return persistLiveDecision(t.Context(), tx, state.Sessions, &decision)
}

type storedTimeBoundScenario struct {
	edit    func(*endedFixture)
	payload func(slot time.Time) contract.VideoLiveCheckV1
	outcome string
	reason  string
}

// archiveAt은 기본 저장 시각(S-2시간 시작, S-20분 종료)에서 어긋난 아카이브 시각을 만든다.
func archiveAt(startOffset, endOffset time.Duration) func(time.Time) contract.VideoLiveCheckV1 {
	return func(slot time.Time) contract.VideoLiveCheckV1 {
		return archiveVideoCheck(slot.Add(-2*time.Hour+startOffset), slot.Add(-20*time.Minute+endOffset))
	}
}

func storedTimeBoundScenarios() map[string]storedTimeBoundScenario {
	return map[string]storedTimeBoundScenario{
		"explicit end within upper slack": {payload: archiveAt(0, 10*time.Minute-time.Second), outcome: finalizationFinalized},
		"explicit end after upper slack":  {payload: archiveAt(0, 10*time.Minute+time.Second), outcome: finalizationConflict, reason: finalizationRuleEndAfterStoredEnd},
		"scoped absence after upper slack": {
			edit:    func(f *endedFixture) { f.endReason = string(live.EndReasonScopedAbsence) },
			payload: archiveAt(0, 10*time.Minute+time.Second), outcome: finalizationConflict, reason: finalizationRuleEndAfterStoredEnd,
		},
		"unresolvable end has no upper bound": {
			edit: func(f *endedFixture) {
				f.endReason, f.endedAt = string(live.EndReasonUnresolvableVideo), f.endedAt.Add(-time.Hour)
			},
			payload: archiveAt(0, 0), outcome: finalizationFinalized,
		},
		"end before live positive slack": {payload: archiveAt(0, -15*time.Minute-time.Second), outcome: finalizationConflict, reason: finalizationRuleEndBeforePositive},
		"start after tolerance":          {payload: archiveAt(15*time.Minute+time.Second, 0), outcome: finalizationConflict, reason: finalizationRuleStartMismatch},
		"start within tolerance":         {payload: archiveAt(15*time.Minute, 0), outcome: finalizationFinalized},
		// 시작 시각 없이 늦게 감지한 방송은 저장 시작이 감지 시각이라 실제 시작이 훨씬 이르다.
		"late detected start is replaced": {payload: archiveAt(-90*time.Minute, 0), outcome: finalizationFinalized},
		"start unobserved is filled": {
			edit:    func(f *endedFixture) { f.startedAt = nil },
			payload: archiveAt(2*time.Hour-30*time.Minute, 0), outcome: finalizationFinalized,
		},
		"legacy times are not trusted": {
			edit: func(f *endedFixture) {
				f.origin, f.endReason, f.positiveAt, f.endedAt = string(live.OriginLegacyUnknown), "", nil, f.endedAt.Add(-5*time.Hour)
			},
			payload: archiveAt(time.Hour, 0), outcome: finalizationFinalized,
		},
		"provider still live": {
			payload: func(time.Time) contract.VideoLiveCheckV1 { return liveVideoCheck(contract.VideoAvailabilityPublic) },
			outcome: finalizationConflict, reason: finalizationRuleProviderLive,
		},
		"provider channel differs": {
			payload: func(slot time.Time) contract.VideoLiveCheckV1 {
				payload := archiveAt(0, 0)(slot)

				payload.ChannelID = "UC_OTHER"

				return payload
			},
			outcome: finalizationConflict, reason: finalizationRuleChannelMismatch,
		},
	}
}

func TestTimeFinalizationAppliesStoredTimeBounds(t *testing.T) {
	for name, tc := range storedTimeBoundScenarios() {
		t.Run(name, func(t *testing.T) {
			h := startFinalization(t, tc.edit)
			before := loadFinalizationRow(t, h.pool)
			id := h.observe(t, tc.payload(h.proof.ScheduledFor))
			got := loadFinalizationRow(t, h.pool)

			if valueOr(got.outcome) != tc.outcome || (tc.reason != "" && valueOr(got.reason) != tc.reason) || !got.completed {
				t.Fatalf("progress = %s/%s completed=%v, want %s/%s", valueOr(got.outcome), valueOr(got.reason), got.completed, tc.outcome, tc.reason)
			}

			if tc.outcome == finalizationConflict {
				if got.timesSource != timesSourceObserved || !sameTimePtr(got.startedAt, before.startedAt) || !sameTimePtr(got.endedAt, before.endedAt) {
					t.Fatalf("conflict rewrote session times: %s %v~%v", got.timesSource, got.startedAt, got.endedAt)
				}

				assertApplicationDecision(t, h.pool, id, liveSessionEntityKind, videoTimeConflict)

				return
			}

			if got.timesSource != providerTimesSource {
				t.Fatalf("finalized session times_source = %s", got.timesSource)
			}
		})
	}
}

func sameTimePtr(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == want
	}

	return got.Equal(*want)
}

func TestTimeFinalizationRetriesUnknownOnScheduleUntilExhausted(t *testing.T) {
	t.Parallel()

	h := startFinalization(t, nil)
	detected := h.proof.ScheduledFor.Add(-3 * time.Minute)
	unknown := unknownVideoCheck(contract.LiveCheckReasonIdentityMissing)

	id := h.observe(t, unknown)
	assertApplicationDecision(t, h.pool, id, liveSessionEntityKind, videoTimeAttempted)

	for attempt, delay := range []time.Duration{30 * time.Minute, 3 * time.Hour, 24 * time.Hour} {
		got := loadFinalizationRow(t, h.pool)
		next := detected.Add(delay)

		if valueOr(got.outcome) != finalizationPending || got.attempts != int16(attempt+1) || !sameTime(got.nextAttemptAt, next) ||
			valueOr(got.reason) != string(contract.LiveCheckReasonIdentityMissing) {
			t.Fatalf("after attempt %d: %s attempts=%d next=%v reason=%s; want next %v",
				attempt+1, valueOr(got.outcome), got.attempts, got.nextAttemptAt, valueOr(got.reason), next)
		}

		// 예정 시각 전의 관측은 시도로 세지 않는다.
		early := h.observeAt(t, next.Add(-time.Minute), unknown)
		assertApplicationDecision(t, h.pool, early, liveSessionEntityKind, videoLifecycleKeepEnded)

		h.observeAt(t, next, unknown)
	}

	got := loadFinalizationRow(t, h.pool)
	if valueOr(got.outcome) != finalizationExhausted || got.attempts != finalizationMaxAttempts ||
		valueOr(got.reason) != string(contract.LiveCheckReasonIdentityMissing) || got.nextAttemptAt != nil || !got.completed ||
		got.timesSource != timesSourceObserved {
		t.Fatalf("final progress = %s attempts=%d reason=%s next=%v source=%s",
			valueOr(got.outcome), got.attempts, valueOr(got.reason), got.nextAttemptAt, got.timesSource)
	}
}

func TestTimeFinalizationEarlyArchiveStillFinalizes(t *testing.T) {
	t.Parallel()

	h := startFinalization(t, nil)
	h.observe(t, unknownVideoCheck(contract.LiveCheckReasonIdentityMissing))

	slot := h.proof.ScheduledFor
	h.observeAt(t, slot.Add(5*time.Minute), archiveVideoCheck(slot.Add(-2*time.Hour), slot.Add(-22*time.Minute)))

	if got := loadFinalizationRow(t, h.pool); valueOr(got.outcome) != finalizationFinalized || got.timesSource != providerTimesSource {
		t.Fatalf("archive before the next attempt slot = %s/%s; want finalized", valueOr(got.outcome), got.timesSource)
	}
}

func TestTimeFinalizationDefersRequestFailuresSeparately(t *testing.T) {
	t.Parallel()

	h := startFinalization(t, nil)
	failed := unknownVideoCheck(contract.LiveCheckReasonRequestFailed)
	at := h.proof.ScheduledFor.Add(time.Second)

	for deferral := 1; deferral <= finalizationMaxDeferrals; deferral++ {
		if deferral == 1 {
			h.observe(t, failed)
		} else {
			h.observeAt(t, at, failed)
		}

		got := loadFinalizationRow(t, h.pool)
		if valueOr(got.outcome) != finalizationPending || got.attempts != 0 || got.deferrals != int16(deferral) ||
			!sameTime(got.nextAttemptAt, at.Add(finalizationDeferral)) {
			t.Fatalf("deferral %d: %s attempts=%d deferrals=%d next=%v", deferral, valueOr(got.outcome), got.attempts, got.deferrals, got.nextAttemptAt)
		}

		at = at.Add(finalizationDeferral)
	}

	h.observeAt(t, at, failed)

	got := loadFinalizationRow(t, h.pool)
	if valueOr(got.outcome) != finalizationExhausted || valueOr(got.reason) != string(contract.LiveCheckReasonRequestFailed) ||
		got.deferrals != finalizationMaxDeferrals || got.attempts != 0 {
		t.Fatalf("request failures beyond the limit = %s/%s deferrals=%d attempts=%d",
			valueOr(got.outcome), valueOr(got.reason), got.deferrals, got.attempts)
	}
}

func TestTimeFinalizationEndsMembersOnlyWithoutTimes(t *testing.T) {
	t.Parallel()

	h := startFinalization(t, nil)
	id := h.observe(t, contract.VideoLiveCheckV1{
		VideoID: testVideoID, ChannelID: testChannelID, IdentityConfirmed: true,
		Availability: contract.VideoAvailabilityMembersOnly, Method: contract.VideoAvailabilityMethodPlayerMembersOnly,
		Coverage: contract.VideoLiveCheckCoverageV1{VideoID: testVideoID},
	})

	got := loadFinalizationRow(t, h.pool)
	if valueOr(got.outcome) != finalizationMembersOnly || !got.completed || got.timesSource != timesSourceObserved {
		t.Fatalf("members-only without times = %s completed=%v source=%s", valueOr(got.outcome), got.completed, got.timesSource)
	}

	assertApplicationDecision(t, h.pool, id, liveSessionEntityKind, videoTimeAttempted)
}

func TestTimeFinalizationReplayDoesNotCountAttemptTwice(t *testing.T) {
	t.Parallel()

	h := startFinalization(t, nil)
	id := h.observe(t, unknownVideoCheck(contract.LiveCheckReasonIdentityMissing))

	replay, err := NewRepository(h.pool).RequestReplay(t.Context(), ReplayInput{
		ObservationID: id, RequestedBy: testReplayOperator, Reason: "finalization replay",
	})
	if err != nil || !replay.Applied {
		t.Fatalf("request replay: %#v err=%v", replay, err)
	}

	consumeLiveChecks(t.Context(), t, h.consumer)

	if got := loadFinalizationRow(t, h.pool); got.attempts != 1 || valueOr(got.outcome) != finalizationPending {
		t.Fatalf("replayed observation counted again: attempts=%d outcome=%s", got.attempts, valueOr(got.outcome))
	}
}

func TestTimeFinalizationLeavesNonCandidatesEnded(t *testing.T) {
	for name, edit := range map[string]func(*endedFixture){
		"canceled before live": func(f *endedFixture) {
			f.endReason, f.startedAt, f.positiveAt = string(live.EndReasonCancelledBeforeLive), nil, nil
		},
		"already provider":          func(f *endedFixture) { f.timesSrc = providerTimesSource },
		"no start or live evidence": func(f *endedFixture) { f.startedAt, f.positiveAt = nil, nil },
		"ended before new window": func(f *endedFixture) {
			f.detectedAt = f.detectedAt.Add(-finalizationNewWindow - time.Minute)
			f.startedAt, f.endedAt = new(f.detectedAt.Add(-2*time.Hour)), f.detectedAt
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := startFinalization(t, edit)
			slot := h.proof.ScheduledFor
			before := loadFinalizationRow(t, h.pool)
			id := h.observe(t, archiveVideoCheck(slot.Add(-2*time.Hour), slot.Add(-4*time.Minute)))

			assertApplicationDecision(t, h.pool, id, liveSessionEntityKind, videoLifecycleKeepEnded)
			assertTableCount(t, h.pool, "youtube_live_time_finalizations", 0)

			if got := loadFinalizationRow(t, h.pool); got.timesSource != before.timesSource || !sameTimePtr(got.endedAt, before.endedAt) {
				t.Fatalf("non-candidate changed: %s %v", got.timesSource, got.endedAt)
			}
		})
	}
}
