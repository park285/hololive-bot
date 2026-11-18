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

	TimeFinalizationNewWindow         = 48 * time.Hour
	TimeFinalizationFirstAttemptDelay = 3 * time.Minute

	finalizationMaxAttempts    = 4
	finalizationMaxDeferrals   = 12
	finalizationDeferral       = 10 * time.Minute
	finalizationEndSlack       = 10 * time.Minute
	finalizationStartTolerance = 15 * time.Minute
)

var finalizationRetryDelays = [finalizationMaxAttempts]time.Duration{
	TimeFinalizationFirstAttemptDelay, 30 * time.Minute, 3 * time.Hour, 24 * time.Hour,
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
	Row         *timeFinalizationRow
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

func finalizeEndedSessionTimes(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
) (string, error) {
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

	if err := tx.QueryRow(ctx, repositoryLiveTimeFinalizationStateSQL, videoID).Scan(
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

	return session.StatusObservedAt != nil && !session.StatusObservedAt.Before(observedAt.Add(-TimeFinalizationNewWindow))
}

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
		return finalizationVerdict{kind: verdictConflict, reason: finalizationRuleProviderLive}
	}

	endedAt, ok := payload.VerifiedEndedAt()
	if !ok {
		if payload.Availability == contract.VideoAvailabilityMembersOnly {
			return finalizationVerdict{kind: verdictMembersOnly}
		}

		return finalizationVerdict{kind: verdictUnknown, reason: finalizationReasonEndMissing}
	}

	if payload.StartedAt == nil {
		return finalizationVerdict{kind: verdictUnknown, reason: finalizationReasonStartMissing}
	}

	if rule := storedTimeConflict(session, *payload.StartedAt, endedAt); rule != "" {
		return finalizationVerdict{kind: verdictConflict, reason: rule}
	}

	return finalizationVerdict{kind: verdictFinalize, startedAt: *payload.StartedAt, endedAt: endedAt}
}

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

	err := tx.QueryRow(ctx, repositoryLiveTimeFinalizationWriteSQL,
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
