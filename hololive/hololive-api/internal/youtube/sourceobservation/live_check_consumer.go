package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	channelLiveCheckEntityKind  = "youtube_channel_live_check"
	videoAvailabilityEntityKind = "youtube_video_availability"
	liveSessionEntityKind       = "youtube_live_session"

	liveCheckDecisionApplied         = "APPLIED"
	liveCheckDecisionNotNewer        = "NOT_NEWER_RETAINED"
	liveCheckDecisionNoSession       = "NO_CANONICAL_SESSION"
	videoLifecycleKeepEnded          = "KEEP_ENDED"
	videoLifecycleSessionNotLive     = "SESSION_NOT_LIVE"
	videoLifecycleIdentityUnverified = "IDENTITY_UNCONFIRMED"
	videoLifecycleIdentityMismatch   = "IDENTITY_MISMATCH"
	videoLifecycleUntrusted          = "LIFECYCLE_UNTRUSTED"
	videoLifecycleUnavailableOnly    = "PUBLIC_UNAVAILABLE_NOT_POSITIVE"
	videoLifecycleNoFact             = "NO_LIFECYCLE_FACT"
	videoLifecycleInvalidEnd         = "INVALID_END_TIMELINE"
	videoLifecycleNewerEndRetained   = "NEWER_END_EVIDENCE_RETAINED"
)

func (c *Consumer) liveCheckReconcile(kind contract.ObservationKind) (func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error), bool) {
	if kind == contract.KindChannelLiveCheck {
		return c.reconcileChannelLiveCheck, true
	}

	if kind == contract.KindVideoLiveCheck {
		return c.reconcileVideoLiveCheck, true
	}

	return nil, false
}

func (c *Consumer) reconcileChannelLiveCheck(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	var payload contract.ChannelLiveCheckV1

	if err := jsonv2.Unmarshal(claimed.Payload, &payload); err != nil {
		return ReconcileResult{}, fmt.Errorf("decode channel live check payload: %w", err)
	}

	applied, err := upsertLatestLiveCheck(ctx, tx, repositoryChannelLiveCheckUpsertSQL,
		payload.ChannelID, claimed.Provider, string(payload.Outcome), nullableText(payload.SelectedVideoID),
		payload.ChannelIdentityConfirmed, nullableText(string(payload.UnknownReason)),
		claimed.ID, claimed.EvidenceSHA256, claimed.ScheduledFor, claimed.EffectiveAt,
		claimed.ObservedAt, claimed.ReceivedAt,
	)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("upsert channel live check: %w", err)
	}

	return ReconcileResult{Applications: []Application{{
		EntityKind: channelLiveCheckEntityKind,
		EntityKey:  payload.ChannelID,
		Decision:   latestLiveCheckDecision(applied),
	}}}, nil
}

func (c *Consumer) reconcileVideoLiveCheck(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	var payload contract.VideoLiveCheckV1

	if err := jsonv2.Unmarshal(claimed.Payload, &payload); err != nil {
		return ReconcileResult{}, fmt.Errorf("decode video live check payload: %w", err)
	}

	state, err := loadLiveState(ctx, tx, nil, []string{payload.VideoID})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load live state: %w", err)
	}

	session, ok := state.Sessions[payload.VideoID]
	if !ok || !session.Present {
		return ReconcileResult{Applications: []Application{{
			EntityKind: videoAvailabilityEntityKind, EntityKey: payload.VideoID, Decision: liveCheckDecisionNoSession,
		}}}, nil
	}

	applied, err := persistVideoAvailability(ctx, tx, claimed, &payload, session.ChannelID)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("upsert video availability: %w", err)
	}

	applications := []Application{{
		EntityKind: videoAvailabilityEntityKind, EntityKey: payload.VideoID, Decision: latestLiveCheckDecision(applied),
	}}
	if !applied {
		return ReconcileResult{Applications: applications}, nil
	}

	var result ReconcileResult

	if session.Status == domain.LiveStatusEnded {
		result, err = reconcileEndedVideoTimes(ctx, tx, claimed, &payload, &session, applications)
	} else {
		result, err = c.reduceVideoLiveCheck(ctx, tx, claimed, &payload, &state, &session, applications)
	}

	if err != nil {
		return ReconcileResult{}, err
	}

	result.LiveRuntimeVideoIDs = append(result.LiveRuntimeVideoIDs, payload.VideoID)

	return result, nil
}

func (c *Consumer) reduceVideoLiveCheck(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	payload *contract.VideoLiveCheckV1,
	state *live.State,
	session *live.SessionState,
	applications []Application,
) (ReconcileResult, error) {
	fact, skipped, err := c.videoLifecycleOrUnresolvableFact(ctx, tx, claimed, payload, session)
	if err != nil {
		return ReconcileResult{}, err
	}

	if pending, exists := state.PendingEnds[payload.VideoID]; skipped == "" && exists && pending.EffectiveAt.After(claimed.EffectiveAt) {
		skipped = videoLifecycleNewerEndRetained
	}

	if skipped != "" {
		applications = append(applications, Application{
			EntityKind: liveSessionEntityKind, EntityKey: payload.VideoID, Decision: skipped,
		})

		return ReconcileResult{Applications: applications}, nil
	}

	decision, err := live.Reduce(*state, videoLiveEvidence(claimed, &fact), c.liveGrace, claimed.ReceivedAt)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reduce: %w", err)
	}

	if persistErr := persistLiveDecision(ctx, tx, state.Sessions, &decision); persistErr != nil {
		return ReconcileResult{}, fmt.Errorf("persist live decision: %w", persistErr)
	}

	return ReconcileResult{
		Applications:        append(applications, applicationsOf(decision.Applications)...),
		LiveRuntimeVideoIDs: liveDecisionRuntimeVideoIDs(&decision),
	}, nil
}

func persistVideoAvailability(ctx context.Context, tx dbx.Tx, claimed *Observation, payload *contract.VideoLiveCheckV1, channelID string) (bool, error) {
	availability := canonicalVideoAvailability(payload, channelID)

	return upsertLatestLiveCheck(ctx, tx, repositoryVideoAvailabilityUpsertSQL,
		payload.VideoID, availability.channelID, claimed.Provider, availability.identityConfirmed,
		string(availability.availability), string(availability.method), nullableText(string(availability.unknownReason)),
		claimed.ID, claimed.EvidenceSHA256, claimed.ScheduledFor, claimed.EffectiveAt, claimed.ObservedAt, claimed.ReceivedAt,
	)
}

type videoAvailabilityRecord struct {
	channelID         string
	identityConfirmed bool
	availability      contract.VideoAvailability
	method            contract.VideoAvailabilityMethod
	unknownReason     contract.LiveCheckUnknownReason
}

func canonicalVideoAvailability(payload *contract.VideoLiveCheckV1, canonicalChannelID string) videoAvailabilityRecord {
	if payload.IdentityConfirmed && payload.ChannelID != canonicalChannelID {
		return videoAvailabilityRecord{
			channelID:     canonicalChannelID,
			availability:  contract.VideoAvailabilityUnknown,
			method:        contract.VideoAvailabilityMethodUnknown,
			unknownReason: contract.LiveCheckReasonIdentityMismatch,
		}
	}

	return videoAvailabilityRecord{
		channelID:         canonicalChannelID,
		identityConfirmed: payload.IdentityConfirmed,
		availability:      payload.Availability,
		method:            payload.Method,
		unknownReason:     payload.UnknownReason,
	}
}

func videoLifecycleFact(
	observation *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
	dbNow time.Time,
	grace time.Duration,
) (live.SessionFact, string) {
	if reason := videoLifecycleGate(payload, session); reason != "" {
		return live.SessionFact{}, reason
	}

	if positiveAtOrAfter(session, observation.EffectiveAt) {
		return live.SessionFact{}, videoLifecycleNewerEndRetained
	}

	if payload.CurrentlyLive() {
		if payload.Availability == contract.VideoAvailabilityPublicUnavailable {
			return live.SessionFact{}, videoLifecycleUnavailableOnly
		}

		positive := contract.LiveSessionV1{
			VideoID: payload.VideoID, ChannelID: session.ChannelID, Status: string(domain.LiveStatusLive), StartedAt: payload.StartedAt,
		}

		return live.SessionFact{
			VideoID:            payload.VideoID,
			ChannelID:          session.ChannelID,
			Status:             string(domain.LiveStatusLive),
			StartedAt:          payload.StartedAt,
			LiveStartConfirmed: liveStartConfirmed(observation.Provider, &positive),
		}, ""
	}

	if session.Status == domain.LiveStatusUpcoming && verifiedWaitingState(payload) {
		return live.SessionFact{
			VideoID: payload.VideoID, ChannelID: session.ChannelID, Status: string(domain.LiveStatusUpcoming),
			ScheduledAt: payload.ScheduledAt,
		}, ""
	}

	endedAt, ok := payload.VerifiedEndedAt()
	if !ok {
		return live.SessionFact{}, videoLifecycleNoFact
	}

	if !validVideoEndTimeline(endedAt, session, observation.ObservedAt) {
		return live.SessionFact{}, videoLifecycleInvalidEnd
	}

	verifiedTerminal := session.Clock.LastLivePositiveAt == nil
	if verifiedTerminal && live.TerminalEndBlockedByPositive(&session.Clock, endedAt, dbNow, grace) {
		return live.SessionFact{}, videoLifecycleInvalidEnd
	}

	return live.SessionFact{
		VideoID:          payload.VideoID,
		ChannelID:        session.ChannelID,
		Status:           string(domain.LiveStatusEnded),
		EndedAt:          &endedAt,
		VerifiedTerminal: verifiedTerminal,
	}, ""
}

func (c *Consumer) videoLifecycleOrUnresolvableFact(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
) (live.SessionFact, string, error) {
	fact, skipped := videoLifecycleFact(claimed, payload, session, claimed.ReceivedAt, c.liveGrace)
	if !unresolvableVideoCandidate(payload, session, skipped) {
		return fact, skipped, nil
	}

	fact, err := c.unresolvableVideoFact(ctx, tx, claimed, payload, session)
	if err != nil {
		return live.SessionFact{}, "", fmt.Errorf("unresolvable video fact: %w", err)
	}

	return fact, "", nil
}

func unresolvableVideoCandidate(payload *contract.VideoLiveCheckV1, session *live.SessionState, skipped string) bool {
	if !live.UnresolvableTrackable(session) {
		return false
	}

	identityMissing := skipped == videoLifecycleIdentityUnverified && payload.UnknownReason == contract.LiveCheckReasonIdentityMissing

	if session.Status == domain.LiveStatusUpcoming {
		return identityMissing || skipped == videoLifecycleIdentityMismatch
	}

	return identityMissing
}

func (c *Consumer) unresolvableVideoFact(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
) (live.SessionFact, error) {
	fact := live.SessionFact{VideoID: payload.VideoID, ChannelID: session.ChannelID, Status: live.StatusUnresolvable}

	since := session.Clock.UnresolvableSince
	if session.Status != domain.LiveStatusLive || since == nil || claimed.EffectiveAt.Before(since.Add(c.unresolvableGrace)) {
		return fact, nil
	}

	var negativeAt time.Time

	err := tx.QueryRow(ctx, repositoryChannelLiveNegativeSQL,
		session.ChannelID, *session.Clock.LastLivePositiveAt, claimed.EffectiveAt).Scan(&negativeAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fact, nil
	}

	if err != nil {
		return live.SessionFact{}, fmt.Errorf("load channel live negative: %w", err)
	}

	fact.VerifiedTerminal = true

	return fact, nil
}

func positiveAtOrAfter(session *live.SessionState, at time.Time) bool {
	for _, positive := range []*time.Time{session.Clock.LastUpcomingPositiveAt, session.Clock.LastLivePositiveAt} {
		if positive != nil && !positive.Before(at) {
			return true
		}
	}

	return false
}

func verifiedWaitingState(payload *contract.VideoLiveCheckV1) bool {
	return payload.WaitingStateConfirmed != nil && *payload.WaitingStateConfirmed &&
		payload.IsUpcoming != nil && *payload.IsUpcoming && payload.IsLiveNow != nil && !*payload.IsLiveNow &&
		payload.ScheduledAt != nil && payload.EndedAt == nil && payload.Availability != contract.VideoAvailabilityPublicUnavailable
}

func videoLifecycleGate(payload *contract.VideoLiveCheckV1, session *live.SessionState) string {
	switch {
	case session.Status == domain.LiveStatusEnded:
		return videoLifecycleKeepEnded
	case session.Status != domain.LiveStatusLive && session.Status != domain.LiveStatusUpcoming:
		return videoLifecycleSessionNotLive
	case !payload.IdentityConfirmed:
		return videoLifecycleIdentityUnverified
	case payload.ChannelID != session.ChannelID:
		return videoLifecycleIdentityMismatch
	case !payload.LifecycleFactsTrusted():
		return videoLifecycleUntrusted
	default:
		return ""
	}
}

func validVideoEndTimeline(endedAt time.Time, session *live.SessionState, observedAt time.Time) bool {
	if session.StartedAt != nil && endedAt.Before(*session.StartedAt) {
		return false
	}

	return !endedAt.After(observedAt)
}

func videoLiveEvidence(observation *Observation, fact *live.SessionFact) live.Evidence {
	return live.Evidence{
		Kind:           observation.ObservationKind,
		ObservationID:  observation.ID,
		ObservationKey: observation.ObservationKey,
		EvidenceSHA256: observation.EvidenceSHA256,
		ScopeSHA256:    observation.ScopeSHA256,
		ScheduledFor:   observation.ScheduledFor,
		EffectiveAt:    observation.EffectiveAt,
		ReceivedAt:     observation.ReceivedAt,
		Completeness:   contract.CompletenessPartial,
		Continuity:     observation.Continuity,
		Sessions:       []live.SessionFact{*fact},
	}
}

func upsertLatestLiveCheck(ctx context.Context, tx dbx.Tx, query string, args ...any) (bool, error) {
	var key string

	err := tx.QueryRow(ctx, query, args...).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("upsert latest live check: %w", err)
	}

	return true, nil
}

func latestLiveCheckDecision(applied bool) string {
	if applied {
		return liveCheckDecisionApplied
	}

	return liveCheckDecisionNotNewer
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}

	return value
}
