package content

import (
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
)

type reduceSession struct {
	state         *State
	evidence      *Evidence
	grace         time.Duration
	applied       map[string]Entity
	fieldUpdates  []Entity
	notifications []NotificationIntent
	conflicts     []Conflict
	applications  []Application
}

func Reduce(state State, evidence Evidence, grace time.Duration) (Decision, error) {
	if evidence.Kind != contract.KindVideoList && evidence.Kind != contract.KindShortsList {
		return Decision{}, fmt.Errorf("content reducer received kind %q", evidence.Kind)
	}

	workingState := state.clone()
	workingEvidence := evidence.clone()
	session := reduceSession{state: &workingState, evidence: &workingEvidence, grace: grace}

	if session.state.Videos == nil {
		session.state.Videos = map[string]EntityState{}
	}

	session.state.Kind = evidence.Kind
	session.state.ChannelID = channelIDOf(session.state, session.evidence)
	applyPositives(&session)

	if scopedNegative(session.evidence) {
		applyNegative(&session)
	}

	if completeEligible(session.evidence) {
		setEarliestComplete(session.state, evidence.EffectiveAt)
	}

	refreshNotifications(&session)

	return session.decision(), nil
}

func channelIDOf(state *State, evidence *Evidence) string {
	if state.ChannelID != "" {
		return state.ChannelID
	}

	if len(evidence.Videos) > 0 {
		return evidence.Videos[0].ChannelID
	}

	if evidence.Coverage.Videos != nil {
		return evidence.Coverage.Videos.ChannelID
	}

	if evidence.Coverage.Shorts != nil {
		return evidence.Coverage.Shorts.ChannelID
	}

	return ""
}

func completeEligible(evidence *Evidence) bool {
	return contract.NegativeEligible(evidence.Completeness, evidence.Continuity)
}

func scopedNegative(evidence *Evidence) bool {
	return completeEligible(evidence) &&
		contract.AbsenceCapabilityFor(evidence.Kind) == contract.AbsenceScoped
}

func setEarliestComplete(state *State, at time.Time) {
	if state.EarliestCompleteAt == nil || at.Before(*state.EarliestCompleteAt) {
		copied := at.UTC()

		state.EarliestCompleteAt = &copied
	}
}

func (s *reduceSession) decision() Decision {
	decision := Decision{
		Videos:             appliedEntities(s.applied),
		FieldUpdates:       s.fieldUpdates,
		Notifications:      s.notifications,
		Watermark:          watermarkOf(s.state, s.evidence),
		Clocks:             clocksOf(s.state),
		AbsenceSlot:        absenceSlotOf(s.state, s.evidence),
		EarliestCompleteAt: s.state.EarliestCompleteAt,
		Conflicts:          s.conflicts,
		Applications:       append(headApplication(s.state), s.applications...),
	}

	decision.Tracking = shortsTrackingOf(decision.Notifications)

	return boundApplications(&decision)
}

func markApplied(session *reduceSession, entity Entity) {
	if session.applied == nil {
		session.applied = map[string]Entity{}
	}

	session.applied[entity.VideoID] = entity
}

func appliedEntities(applied map[string]Entity) []Entity {
	result := make([]Entity, 0, len(applied))
	for _, entity := range applied {
		result = append(result, entity)
	}

	return result
}

func clocksOf(state *State) []EntityState {
	result := make([]EntityState, 0, len(state.Videos))
	for videoID := range state.Videos {
		item := state.Videos[videoID]

		result = append(result, item)
	}

	return result
}

func watermarkType(kind contract.ObservationKind) domain.WatermarkType {
	if kind == contract.KindShortsList {
		return domain.WatermarkTypeShort
	}

	return domain.WatermarkTypeVideo
}

func outboxKind(kind contract.ObservationKind) domain.OutboxKind {
	if kind == contract.KindShortsList {
		return domain.OutboxKindNewShort
	}

	return domain.OutboxKindNewVideo
}

func notificationContentID(kind contract.ObservationKind, videoID string) string {
	return polling.NormalizeContentID(outboxKind(kind), videoID)
}
