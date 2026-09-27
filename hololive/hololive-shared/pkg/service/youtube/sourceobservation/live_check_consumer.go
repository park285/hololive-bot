package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
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

// reconcileVideoLiveCheck는 요청 영상의 canonical 상태만 잠그고 가용성을 기록한다. 수명 전이는
// canonical 채널이 일치하는 LIVE 세션의 신뢰 가능한 현재 LIVE 또는 검증된 종료 사실만 reducer로 보낸다.
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

	fact, skipped := videoLifecycleFact(claimed, &payload, &session)
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
) (live.SessionFact, string) {
	if reason := videoLifecycleGate(payload, session); reason != "" {
		return live.SessionFact{}, reason
	}

	if payload.CurrentlyLive() {
		if payload.Availability == contract.VideoAvailabilityPublicUnavailable {
			return live.SessionFact{}, videoLifecycleUnavailableOnly
		}

		positive := contract.LiveSessionV1{
			VideoID: payload.VideoID, ChannelID: session.ChannelID, Status: string(live.StatusLive), StartedAt: payload.StartedAt,
		}

		return live.SessionFact{
			VideoID:            payload.VideoID,
			ChannelID:          session.ChannelID,
			Status:             string(live.StatusLive),
			StartedAt:          payload.StartedAt,
			LiveStartConfirmed: liveStartConfirmed(observation.Provider, &positive),
		}, ""
	}

	endedAt, ok := payload.VerifiedEndedAt()
	if !ok {
		return live.SessionFact{}, videoLifecycleNoFact
	}

	if !validVideoEndTimeline(endedAt, session, observation.ObservedAt) {
		return live.SessionFact{}, videoLifecycleInvalidEnd
	}

	return live.SessionFact{
		VideoID:   payload.VideoID,
		ChannelID: session.ChannelID,
		Status:    string(live.StatusEnded),
		EndedAt:   &endedAt,
	}, ""
}

// videoLifecycleGate는 ENDED 보존, canonical LIVE, 채널 identity, 수명 사실 신뢰를 차례로 확인한다.
// 가용성만 미상인 경우 외의 UNKNOWN은 reducer를 호출하지 않는다. 저장된 종료도 정산하기 때문이다.
func videoLifecycleGate(payload *contract.VideoLiveCheckV1, session *live.SessionState) string {
	switch {
	case session.Status == live.StatusEnded:
		return videoLifecycleKeepEnded
	case session.Status != live.StatusLive:
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
