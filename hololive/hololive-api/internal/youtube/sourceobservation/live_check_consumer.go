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

// liveCheckReconcile은 absence 권한이 없는 라이브 확인 kind를 전용 canonical 경로로 보낸다.
func (c *Consumer) liveCheckReconcile(kind contract.ObservationKind) (func(context.Context, dbx.Tx, *Observation) (ReconcileResult, error), bool) {
	if kind == contract.KindChannelLiveCheck {
		return c.reconcileChannelLiveCheck, true
	}

	if kind == contract.KindVideoLiveCheck {
		return c.reconcileVideoLiveCheck, true
	}

	return nil, false
}

// reconcileChannelLiveCheck는 채널별 최신 /live 판정만 갱신한다. LiveQuery coverage 전용이므로
// live 상태를 읽거나 reducer·finalizer·pending·absence slot을 만들지 않는다.
func (c *Consumer) reconcileChannelLiveCheck(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	var payload contract.ChannelLiveCheckV1

	if err := jsonv2.Unmarshal(claimed.Payload, &payload); err != nil {
		return ReconcileResult{}, fmt.Errorf("decode channel live check payload: %w", err)
	}

	applied, err := upsertLatestLiveCheck(ctx, tx, mustSQL("repository_channel_live_check_upsert.sql"),
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

// reconcileVideoLiveCheck는 요청 영상의 canonical 상태만 잠그고 가용성을 기록한다.
// Schema 2/generation 2는 UPCOMING의 검증된 예정·현재 LIVE·명시적 종료도 같은 owner에서 수용한다.
func (c *Consumer) reconcileVideoLiveCheck(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
) (ReconcileResult, error) {
	var payload contract.VideoLiveCheckV1

	if err := jsonv2.Unmarshal(claimed.Payload, &payload); err != nil {
		return ReconcileResult{}, fmt.Errorf("decode video live check payload: %w", err)
	}

	// 채널 전체가 아니라 요청 영상의 session→head→pending 행만 기존 잠금 순서로 잠근다.
	state, err := loadLiveState(ctx, tx, nil, []string{payload.VideoID})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("load live state: %w", err)
	}

	session, ok := state.Sessions[payload.VideoID]
	if !ok || !session.Present {
		// canonical 채널을 알 수 없는 영상은 가용성도 수명 상태도 새로 만들지 않는다.
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

	fact, skipped, err := c.videoLifecycleOrUnresolvableFact(ctx, tx, claimed, &payload, &session)
	if err != nil {
		return ReconcileResult{}, err
	}

	if pending, exists := state.PendingEnds[payload.VideoID]; skipped == "" && exists && pending.EffectiveAt.After(claimed.EffectiveAt) {
		// 오래된 영상 확인이 더 새롭고 종료 시각 없는 pending을 정산해 슬롯 시각으로 끝내게 하지 않는다.
		skipped = videoLifecycleNewerEndRetained
	}

	if skipped != "" {
		applications = append(applications, Application{
			EntityKind: liveSessionEntityKind, EntityKey: payload.VideoID, Decision: skipped,
		})

		return ReconcileResult{Applications: applications}, nil
	}

	decision, err := live.Reduce(state, videoLiveEvidence(claimed, &fact), c.liveGrace, claimed.ReceivedAt)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reduce: %w", err)
	}

	if persistErr := persistLiveDecision(ctx, tx, &decision); persistErr != nil {
		return ReconcileResult{}, fmt.Errorf("persist live decision: %w", persistErr)
	}

	return ReconcileResult{Applications: append(applications, mapLiveApplications(decision.Applications)...)}, nil
}

func persistVideoAvailability(ctx context.Context, tx dbx.Tx, claimed *Observation, payload *contract.VideoLiveCheckV1, channelID string) (bool, error) {
	availability := canonicalVideoAvailability(payload, channelID)

	return upsertLatestLiveCheck(ctx, tx, mustSQL("repository_video_availability_upsert.sql"),
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

// canonicalVideoAvailability는 canonical session 채널로 가용성 행을 만든다. 응답 채널이 canonical
// 채널과 다르면 이전 판정을 계속 쓰지 않도록 identity_mismatch UNKNOWN으로 대체한다.
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

// videoLifecycleFact는 reducer에 보낼 단일 수명 사실을 고른다. 빈 사유는 전달 가능함을 뜻한다.
// 공개 불가만으로는 LIVE를 갱신하지 않으며, 종료는 upstream 종료 시각이 canonical 시작 이후이고
// 관측 시각 이전일 때만 해당 영상의 명시적 종료로 전달한다.
func videoLifecycleFact(
	observation *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
	dbNow time.Time,
	grace time.Duration,
) (live.SessionFact, string) {
	newLifecycle := videoLifecycleGeneration(observation)
	if reason := videoLifecycleGate(payload, session, newLifecycle); reason != "" {
		return live.SessionFact{}, reason
	}

	if newLifecycle && positiveAtOrAfter(session, observation.EffectiveAt) {
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

	if newLifecycle && session.Status == domain.LiveStatusUpcoming && verifiedWaitingState(payload) {
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

	// LIVE positive clock이 있으면 reducer가 관측 시각 기준 positive 비교와 grace로 끝낸다.
	// 시작 미관측 terminal 경로는 grace 없이 바로 끝내므로 ended_at 이후의 positive가 grace 안에서
	// 관측된 동안만 거부한다. provider positive의 EffectiveAt은 수집 예정 시각이라 실제 종료보다
	// 늦을 수 있어, 신선도 없이 ended_at과 직접 비교하면 검증된 종료를 영구히 거부한다.
	verifiedTerminal := newLifecycle && session.Clock.LastLivePositiveAt == nil
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

func videoLifecycleGeneration(observation *Observation) bool {
	return observation.ContractGeneration == contract.VideoLifecycleContractGeneration && observation.SchemaVersion == contract.VideoLifecycleSchemaVersion
}

// videoLifecycleOrUnresolvableFact는 기존 수명 사실을 먼저 구하고, identity를 확인할 수 없어 건너뛴 확인이
// 해소 불가 추적 대상이면 해소 불가 사실로 대신한다. 다른 건너뛰기 사유는 그대로 돌려준다.
func (c *Consumer) videoLifecycleOrUnresolvableFact(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
) (live.SessionFact, string, error) {
	fact, skipped := videoLifecycleFact(claimed, payload, session, claimed.ReceivedAt, c.liveGrace)
	if skipped != videoLifecycleIdentityUnverified || !unresolvableVideoCandidate(claimed, payload, session) {
		return fact, skipped, nil
	}

	fact, err := c.unresolvableVideoFact(ctx, tx, claimed, payload, session)
	if err != nil {
		return live.SessionFact{}, "", fmt.Errorf("unresolvable video fact: %w", err)
	}

	return fact, "", nil
}

// unresolvableVideoCandidate는 시작을 관측한 LIVE 세션의 identity_missing 확인만 해소 불가 추적에 들인다.
// 비공개·삭제 전환 영상은 익명 player에 videoDetails가 없어 다른 UNKNOWN 사유와 달리 identity_missing만 남긴다.
// 그 밖의 identity_mismatch·request_failed 등은 수명을 바꾸지 않는 기존 IDENTITY_UNCONFIRMED로 남는다.
func unresolvableVideoCandidate(observation *Observation, payload *contract.VideoLiveCheckV1, session *live.SessionState) bool {
	return videoLifecycleGeneration(observation) && !payload.IdentityConfirmed &&
		payload.UnknownReason == contract.LiveCheckReasonIdentityMissing &&
		session.Status == domain.LiveStatusLive && session.Clock.LastLivePositiveAt != nil
}

// unresolvableVideoFact는 identity_missing 확인을 reducer의 해소 불가 사실로 바꾼다. 첫 추적 뒤 설정된 지속 시간이
// 지났고 같은 채널의 /live 확인이 마지막 positive 이후의 신선한 identity 확인 음성이면 종료를 검증한다(VerifiedTerminal).
// 채널 확인 최신값은 읽기만 하며 reducer로 보내지 않는다. 로봇 확인이 player를 막아도 방송 중인 채널의 /live는
// 방송으로 이동해 음성이 나오지 않으므로, 음성은 그 채널이 지금 공개 방송 중이 아니라는 독립 증거다.
func (c *Consumer) unresolvableVideoFact(
	ctx context.Context,
	tx dbx.Tx,
	claimed *Observation,
	payload *contract.VideoLiveCheckV1,
	session *live.SessionState,
) (live.SessionFact, error) {
	fact := live.SessionFact{VideoID: payload.VideoID, ChannelID: session.ChannelID, Status: live.StatusUnresolvable}

	since := session.Clock.UnresolvableSince
	if since == nil || claimed.EffectiveAt.Before(since.Add(c.unresolvableGrace)) {
		return fact, nil
	}

	var negativeAt time.Time

	err := tx.QueryRow(ctx, mustSQL("repository_channel_live_negative.sql"),
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

// videoLifecycleGate는 ENDED 보존, 세대별 canonical 대상, 채널 identity, 수명 사실 신뢰를 확인한다.
// 가용성만 미상인 경우 외의 UNKNOWN은 reducer를 호출하지 않는다. 저장된 종료도 정산하기 때문이다.
func videoLifecycleGate(payload *contract.VideoLiveCheckV1, session *live.SessionState, newLifecycle bool) string {
	switch {
	case session.Status == domain.LiveStatusEnded:
		return videoLifecycleKeepEnded
	case session.Status != domain.LiveStatusLive && (!newLifecycle || session.Status != domain.LiveStatusUpcoming):
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

// videoLiveEvidence는 PARTIAL video_live_check를 kind 그대로 reducer에 전달한다.
// 부재 coverage가 없으므로 absence slot과 이력은 만들지도 읽지도 않는다.
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

// upsertLatestLiveCheck는 최신값 upsert를 실행하고, 더 새로운 관측이라 반영됐는지 보고한다.
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
