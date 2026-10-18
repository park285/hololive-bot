package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// 종료된 세션의 시작·종료 시각을 영상 확인의 아카이브 시각으로 확정한다. 종료 판정과 reducer는 바꾸지 않고
// 시각과 출처(times_source)만 바꾼다. 대기열 후보 조건은 YouTube plane의 확정 대상 reader와 같아야 한다.
const (
	videoTimeFinalized = "TIME_FINALIZED"
	videoTimeConflict  = "TIME_CONFLICT"
	videoTimeAttempted = "TIME_ATTEMPTED"

	timesSourceObserved = "observed"

	finalizationQueueNew    = "new"
	finalizationPending     = "pending"
	finalizationFinalized   = "finalized"
	finalizationExhausted   = "exhausted"
	finalizationConflict    = "conflict"
	finalizationMembersOnly = "members_only"

	finalizationReasonEndMissing      = "end_time_missing"
	finalizationReasonStartMissing    = "start_time_missing"
	finalizationRuleProviderLive      = "provider_live"
	finalizationRuleChannelMismatch   = "channel_mismatch"
	finalizationRuleEndAfterStoredEnd = "end_after_stored_end"
	finalizationRuleEndBeforePositive = "end_before_live_positive"
	finalizationRuleStartMismatch     = "start_mismatch"

	// 신규 종료는 종료 관측 뒤 이 기간 안에만 진행 행 없이 대기열에 들어간다.
	finalizationNewWindow    = 48 * time.Hour
	finalizationMaxAttempts  = 4
	finalizationMaxDeferrals = 12
	finalizationDeferral     = 10 * time.Minute
	// 저장 종료는 수집 지연(최대 약 8분)만큼 실제 종료보다 늦고, LIVE 확인은 실제 종료보다 이르다.
	finalizationEndSlack       = 10 * time.Minute
	finalizationStartTolerance = 15 * time.Minute
)

// finalizationRetryDelays[n]은 응답 수준 UNKNOWN n회 뒤 다음 시도까지의 retry_base_at 기준 간격이다.
var finalizationRetryDelays = [finalizationMaxAttempts]time.Duration{
	3 * time.Minute, 30 * time.Minute, 3 * time.Hour, 24 * time.Hour,
}

type timeFinalizationRow struct {
	Queue         string
	Outcome       string
	Attempts      int16
	Deferrals     int16
	RetryBaseAt   *time.Time
	NextAttemptAt *time.Time
	ObservationID *int64
	EnqueuedAt    time.Time
}

type timeFinalizationState struct {
	TimesSource string
	// Row가 nil이면 진행 행이 없다.
	Row *timeFinalizationRow
}

type finalizationVerdictKind int

const (
	verdictFinalize finalizationVerdictKind = iota
	verdictConflict
	verdictMembersOnly
	verdictUnknown
	verdictRequestFailed
)

type finalizationVerdict struct {
	kind      finalizationVerdictKind
	reason    string
	startedAt time.Time
	endedAt   time.Time
}

type timeFinalizationWrite struct {
	VideoID           string
	Queue             string
	Outcome           string
	Reason            string
	Attempts          int16
	Deferrals         int16
	RetryBaseAt       *time.Time
	NextAttemptAt     *time.Time
	LastAttemptAt     time.Time
	ObservationID     int64
	ObservedStartedAt *time.Time
	ObservedEndedAt   *time.Time
	StartedAt         *time.Time
	EndedAt           *time.Time
	ExpectAttempts    int16
	ExpectDeferrals   int16
	ExpectObservation *int64
}

// reconcileEndedVideoTimes는 가용성 결정 뒤에 ENDED 세션의 시각 확정 결정을 붙인다.
func reconcileEndedVideoTimes(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
	applications []Application,
) (ReconcileResult, error) {
	decision, err := finalizeEndedSessionTimes(ctx, tx, claimed, payload, session)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("finalize ended session times: %w", err)
	}

	return ReconcileResult{Applications: append(applications, Application{
		EntityKind: liveSessionEntityKind, EntityKey: payload.VideoID, Decision: decision,
	})}, nil
}

// finalizeEndedSessionTimes는 ENDED 세션의 영상 확인을 확정 대기열에 반영하고 결정값을 돌려준다.
// 대기열 후보가 아니거나 반영할 결과가 없으면 기존 KEEP_ENDED다.
func finalizeEndedSessionTimes(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
) (string, error) {
	if !videoLifecycleGeneration(claimed) {
		return videoLifecycleKeepEnded, nil
	}

	state, err := loadTimeFinalizationState(ctx, tx, session.VideoID)
	if err != nil {
		return "", err
	}

	if !timeFinalizationCandidate(session, &state, claimed.ObservedAt) {
		return videoLifecycleKeepEnded, nil
	}

	write, decision, ok := planTimeFinalization(session, &state, evaluateTimeFinalization(payload, session), claimed)
	if !ok {
		return decision, nil
	}

	if err := persistTimeFinalization(ctx, tx, &write); err != nil {
		return "", err
	}

	return decision, nil
}

func loadTimeFinalizationState(ctx context.Context, tx dbx.Tx, videoID string) (timeFinalizationState, error) {
	var (
		state               timeFinalizationState
		queue, outcome      *string
		attempts, deferrals *int16
		retryBase, next     *time.Time
		observationID       *int64
		enqueuedAt          *time.Time
	)

	if err := tx.QueryRow(ctx, mustSQL("repository_live_time_finalization_state.sql"), videoID).Scan(
		&state.TimesSource, &queue, &outcome, &attempts, &deferrals, &retryBase, &next, &observationID, &enqueuedAt,
	); err != nil {
		return timeFinalizationState{}, fmt.Errorf("load live time finalization: %w", err)
	}

	if outcome == nil || queue == nil || attempts == nil || deferrals == nil || enqueuedAt == nil {
		return state, nil
	}

	state.Row = &timeFinalizationRow{
		Queue: *queue, Outcome: *outcome, Attempts: *attempts, Deferrals: *deferrals,
		RetryBaseAt: retryBase, NextAttemptAt: next, ObservationID: observationID, EnqueuedAt: *enqueuedAt,
	}

	return state, nil
}

// timeFinalizationCandidate는 대기열 후보 조건이다. 시작 전 취소와 시작·LIVE 확인이 모두 없는 종료는 확정할
// 시각이 없다. 진행 행이 없으면 종료 관측이 신규 창 안일 때만, 행이 있으면 pending일 때만 후보다.
func timeFinalizationCandidate(session *live.SessionState, state *timeFinalizationState, observedAt time.Time) bool {
	if state.TimesSource != timesSourceObserved {
		return false
	}

	if session.EndReason != nil && *session.EndReason == live.EndReasonCancelledBeforeLive {
		return false
	}

	if session.StartedAt == nil && session.Clock.LastLivePositiveAt == nil {
		return false
	}

	if state.Row != nil {
		return state.Row.Outcome == finalizationPending
	}

	return session.StatusObservedAt != nil && !session.StatusObservedAt.Before(observedAt.Add(-finalizationNewWindow))
}

// evaluateTimeFinalization은 관측을 확정·충돌·멤버십·UNKNOWN·요청 실패 중 하나로 판정한다.
func evaluateTimeFinalization(payload *contract.VideoLiveCheckV1, session *live.SessionState) finalizationVerdict {
	switch {
	case payload.UnknownReason == contract.LiveCheckReasonRequestFailed:
		return finalizationVerdict{kind: verdictRequestFailed, reason: string(payload.UnknownReason)}
	case !payload.IdentityConfirmed:
		return finalizationVerdict{kind: verdictUnknown, reason: string(payload.UnknownReason)}
	case payload.ChannelID != session.ChannelID:
		return finalizationVerdict{kind: verdictConflict, reason: finalizationRuleChannelMismatch}
	case !payload.LifecycleFactsTrusted():
		return finalizationVerdict{kind: verdictUnknown, reason: string(payload.UnknownReason)}
	case payload.CurrentlyLive():
		// 저장된 종료가 틀렸을 수 있다는 증거라 재시도하지 않고 드러낸다.
		return finalizationVerdict{kind: verdictConflict, reason: finalizationRuleProviderLive}
	}

	endedAt, ok := payload.VerifiedEndedAt()
	if !ok {
		if payload.Availability == contract.VideoAvailabilityMembersOnly {
			return finalizationVerdict{kind: verdictMembersOnly}
		}

		return finalizationVerdict{kind: verdictUnknown, reason: finalizationReasonEndMissing}
	}

	// 계약 검증이 started_at ≤ ended_at ≤ observed_at을 이미 강제한다.
	if payload.StartedAt == nil {
		return finalizationVerdict{kind: verdictUnknown, reason: finalizationReasonStartMissing}
	}

	if rule := storedTimeConflict(session, *payload.StartedAt, endedAt); rule != "" {
		return finalizationVerdict{kind: verdictConflict, reason: rule}
	}

	return finalizationVerdict{kind: verdictFinalize, startedAt: *payload.StartedAt, endedAt: endedAt}
}

// storedTimeConflict는 저장 시각과 아카이브 시각의 경계 규칙 위반을 돌려준다. 옛 기록(legacy_unknown)의 저장
// 시각은 신뢰할 의미가 없어 검사하지 않는다. UNRESOLVABLE_VIDEO 종료의 저장값은 실제 종료의 하한이라 상한이 없다.
// 저장 시작은 시작 시각 없는 첫 LIVE 관측이면 감지 시각이라 실제 시작보다 늦을 수만 있으므로, 아카이브 시작이
// 저장 시작보다 늦을 때만 허용폭을 적용한다.
func storedTimeConflict(session *live.SessionState, startedAt, endedAt time.Time) string {
	if session.LifecycleOrigin == live.OriginLegacyUnknown {
		return ""
	}

	if session.EndedAt != nil && session.EndReason != nil &&
		(*session.EndReason == live.EndReasonExplicitEnd || *session.EndReason == live.EndReasonScopedAbsence) &&
		endedAt.After(session.EndedAt.Add(finalizationEndSlack)) {
		return finalizationRuleEndAfterStoredEnd
	}

	if positive := session.Clock.LastLivePositiveAt; positive != nil && endedAt.Before(positive.Add(-finalizationEndSlack)) {
		return finalizationRuleEndBeforePositive
	}

	if session.StartedAt != nil && startedAt.Sub(*session.StartedAt) > finalizationStartTolerance {
		return finalizationRuleStartMismatch
	}

	return ""
}

// planTimeFinalization은 판정을 진행 행 갱신으로 바꾼다. 다음 시도 시각 전에 온 관측은 확정만 반영한다.
// 이른 관측은 종료 전 LIVE 상태를 보고했을 수 있기 때문이다. 같은 관측의 재정산은 가용성 upsert가 먼저 거른다.
func planTimeFinalization(
	session *live.SessionState,
	state *timeFinalizationState,
	verdict finalizationVerdict,
	claimed *Observation,
) (timeFinalizationWrite, string, bool) {
	row := state.Row

	if verdict.kind != verdictFinalize && !timeFinalizationDue(session, row, claimed.ObservedAt) {
		return timeFinalizationWrite{}, videoLifecycleKeepEnded, false
	}

	write := newTimeFinalizationWrite(session, row, claimed)

	switch verdict.kind {
	case verdictFinalize:
		write.Outcome = finalizationFinalized
		write.StartedAt, write.EndedAt = &verdict.startedAt, &verdict.endedAt

		return write, videoTimeFinalized, true
	case verdictConflict:
		write.Outcome, write.Reason = finalizationConflict, verdict.reason

		return write, videoTimeConflict, true
	case verdictMembersOnly:
		write.Outcome = finalizationMembersOnly
	case verdictUnknown:
		write.Reason = verdict.reason
		write.Attempts++

		if write.Attempts >= finalizationMaxAttempts {
			write.Outcome = finalizationExhausted
		} else {
			write.NextAttemptAt = new(write.RetryBaseAt.Add(finalizationRetryDelays[write.Attempts]))
		}
	case verdictRequestFailed:
		write.Reason = verdict.reason

		if write.Deferrals >= finalizationMaxDeferrals {
			write.Outcome = finalizationExhausted
		} else {
			write.Deferrals++

			write.NextAttemptAt = new(claimed.ObservedAt.Add(finalizationDeferral))
		}
	}

	return write, videoTimeAttempted, true
}

// timeFinalizationDue는 관측이 예정된 시도 시각 이후에 이뤄졌는지 본다. 진행 행이 없으면 첫 시도는 종료 관측
// 3분 뒤, 한 번도 결과가 없는 backlog 행은 투입 뒤 어느 관측이든 첫 시도다.
func timeFinalizationDue(session *live.SessionState, row *timeFinalizationRow, observedAt time.Time) bool {
	switch {
	case row == nil:
		return session.StatusObservedAt != nil && !observedAt.Before(session.StatusObservedAt.Add(finalizationRetryDelays[0]))
	case row.NextAttemptAt != nil:
		return !observedAt.Before(*row.NextAttemptAt)
	default:
		return !observedAt.Before(row.EnqueuedAt)
	}
}

// newTimeFinalizationWrite는 잠근 진행 상태에서 시작하는 pending 갱신이다. 신규의 재시도 기준은 종료 관측
// 시각이고, backlog는 첫 결과를 기록한 관측 시각이다.
func newTimeFinalizationWrite(session *live.SessionState, row *timeFinalizationRow, claimed *Observation) timeFinalizationWrite {
	write := timeFinalizationWrite{
		VideoID: session.VideoID, Queue: finalizationQueueNew, Outcome: finalizationPending,
		RetryBaseAt: session.StatusObservedAt, LastAttemptAt: claimed.ObservedAt, ObservationID: claimed.ID,
		ObservedStartedAt: session.StartedAt, ObservedEndedAt: session.EndedAt,
	}

	if row == nil {
		return write
	}

	write.Queue, write.Attempts, write.Deferrals = row.Queue, row.Attempts, row.Deferrals
	write.RetryBaseAt = row.RetryBaseAt
	write.ExpectAttempts, write.ExpectDeferrals, write.ExpectObservation = row.Attempts, row.Deferrals, row.ObservationID

	if write.RetryBaseAt == nil {
		write.RetryBaseAt = new(claimed.ObservedAt)
	}

	return write
}

func persistTimeFinalization(ctx context.Context, tx dbx.Tx, write *timeFinalizationWrite) error {
	var (
		outcome        string
		sessionUpdated bool
	)

	err := tx.QueryRow(ctx, mustSQL("repository_live_time_finalization_write.sql"),
		write.VideoID, write.Queue, write.Outcome, nullableText(write.Reason), write.Attempts, write.Deferrals,
		write.RetryBaseAt, write.NextAttemptAt, write.LastAttemptAt, write.ObservationID,
		write.ObservedStartedAt, write.ObservedEndedAt, write.StartedAt, write.EndedAt,
		write.ExpectAttempts, write.ExpectDeferrals, write.ExpectObservation,
	).Scan(&outcome, &sessionUpdated)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("persist live time finalization: progress row changed after lock")
	}

	if err != nil {
		return fmt.Errorf("persist live time finalization: %w", err)
	}

	if outcome == finalizationFinalized && !sessionUpdated {
		return errors.New("persist live time finalization: finalized session was not updated")
	}

	return nil
}
