package live

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var errIgnoredAbsencesNotLoaded = errors.New("ignored absence history not loaded")

func pendingFromFact(session *reduceSession, fact *SessionFact, kind EndEvidenceKind) PendingEnd {
	return PendingEnd{
		Kind:             kind,
		VideoID:          fact.VideoID,
		ChannelID:        fact.ChannelID,
		ObservationID:    session.evidence.ObservationID,
		EffectiveAt:      session.evidence.EffectiveAt,
		ReceivedAt:       session.evidence.ReceivedAt,
		ScheduledFor:     session.evidence.ScheduledFor,
		EndedAt:          copyOptionalTime(fact.EndedAt),
		NegativeEligible: true,
		ScopeCovers:      true,
	}
}

func recordPendingEnd(session *reduceSession, pending *PendingEnd) {
	existing, ok := session.state.PendingEnds[pending.VideoID]
	if ok && pending.EffectiveAt.Before(existing.EffectiveAt) {
		return
	}

	session.state.PendingEnds[pending.VideoID] = *pending
}

func reapplyStoredAbsences(session *reduceSession, seen map[string]SessionFact) {
	for i := range session.state.AbsenceSlots {
		slot := &session.state.AbsenceSlots[i]
		for videoID := range session.state.Sessions {
			if seenInCurrentSlot(session, slot, videoID, seen) {
				continue
			}

			existing := session.state.Sessions[videoID]
			applyAbsenceToSession(session, &existing, slot)
		}
	}
}

func seenInCurrentSlot(
	session *reduceSession,
	slot *AbsenceSlot,
	videoID string,
	seen map[string]SessionFact,
) bool {
	if !slot.ScheduledFor.Equal(session.evidence.ScheduledFor) {
		return false
	}

	_, present := seen[videoID]

	return present
}

func applyAbsenceToSession(session *reduceSession, existing *SessionState, slot *AbsenceSlot) {
	if session == nil || session.state == nil || existing == nil || slot == nil {
		return
	}

	applyAbsenceToKnownSession(session, existing, slot)
}

func applyAbsenceToKnownSession(session *reduceSession, existing *SessionState, slot *AbsenceSlot) {
	if existing.Status == domain.LiveStatusEnded || !existing.Present {
		return
	}

	if (existing.LifecycleOrigin == OriginMetadataOnly || existing.LifecycleOrigin == OriginLegacyUnknown) &&
		existing.Clock.LastUpcomingPositiveAt == nil && existing.Clock.LastLivePositiveAt == nil {
		return
	}

	covers := session.absenceCovers(slot, existing)
	if !covers {
		return
	}

	ignoredSlots, loaded := ignoredAbsenceSlots(session, existing)
	if !loaded {
		session.fail(fmt.Errorf("apply absence slot %s to %s: %w",
			slot.ScheduledFor.UTC().Format(time.RFC3339), existing.VideoID, errIgnoredAbsencesNotLoaded))

		return
	}

	if _, ignored := ignoredSlots[slot.ScheduledFor.UTC()]; ignored {
		return
	}

	if existing.Clock.LastLivePositiveAt == nil {
		existing.IgnoredAbsences.add(slot.ScheduledFor)

		ignoredSlots[slot.ScheduledFor.UTC()] = struct{}{}
		session.state.Sessions[existing.VideoID] = *existing
		markDirty(session, existing.VideoID)

		return
	}

	applyAbsenceAfterPositive(session, existing, slot, covers)
}

func applyAbsenceAfterPositive(session *reduceSession, existing *SessionState, slot *AbsenceSlot, covers bool) {
	if slot.EffectiveAt.After(*existing.Clock.LastLivePositiveAt) && !replayedAbsence(existing, slot) {
		recordAbsencePending(session, existing, slot, covers)
	}
}

func recordAbsencePending(session *reduceSession, existing *SessionState, slot *AbsenceSlot, covers bool) {
	countAbsenceSlot(existing, slot)

	existing.Clock.LastCompleteAbsenceAt = copyTime(slot.EffectiveAt)
	existing.LastAbsenceObservationID = slot.ObservationID
	existing.LastAbsenceScheduledFor = copyTime(slot.ScheduledFor)
	session.state.Sessions[existing.VideoID] = *existing
	markDirty(session, existing.VideoID)

	pending := PendingEnd{
		Kind:             EndEvidenceScopedAbsence,
		VideoID:          existing.VideoID,
		ChannelID:        existing.ChannelID,
		ObservationID:    slot.ObservationID,
		EffectiveAt:      slot.EffectiveAt,
		ReceivedAt:       slot.ReceivedAt,
		ScheduledFor:     slot.ScheduledFor,
		NegativeEligible: true,
		ScopeCovers:      covers,
	}
	recordPendingEnd(session, &pending)
	reapplyStoredEnds(session, existing.VideoID)
}

func ignoredAbsenceSlots(session *reduceSession, existing *SessionState) (map[time.Time]struct{}, bool) {
	slots, loaded := existing.IgnoredAbsences.Slots()
	if !loaded {
		return nil, false
	}

	if session.ignoredAbsences == nil {
		session.ignoredAbsences = make(map[string]map[time.Time]struct{})
	}

	ignored := session.ignoredAbsences[existing.VideoID]
	if ignored == nil {
		ignored = make(map[time.Time]struct{}, len(slots))
		for _, at := range slots {
			ignored[at.UTC()] = struct{}{}
		}

		session.ignoredAbsences[existing.VideoID] = ignored
	}

	return ignored, true
}

func replayedAbsence(existing *SessionState, slot *AbsenceSlot) bool {
	if existing == nil || slot == nil {
		return true
	}

	if slot.ObservationID != 0 && existing.LastAbsenceObservationID == slot.ObservationID {
		return true
	}

	return sameOptionalTime(existing.FirstAbsenceScheduledFor, &slot.ScheduledFor) ||
		sameOptionalTime(existing.SecondAbsenceScheduledFor, &slot.ScheduledFor)
}

func countAbsenceSlot(entity *SessionState, slot *AbsenceSlot) {
	if entity == nil || slot == nil {
		return
	}

	switch entity.Clock.ConsecutiveAbsenceSlots {
	case 0:
		entity.FirstAbsenceScheduledFor = copyTime(slot.ScheduledFor)
		entity.Clock.ConsecutiveAbsenceSlots = 1
	case 1:
		entity.SecondAbsenceScheduledFor = copyTime(slot.ScheduledFor)
		entity.Clock.ConsecutiveAbsenceSlots = 2
	default:
	}
}

type liveCoverageIndex struct {
	channels map[string]struct{}
	statuses map[string]struct{}
}

func newLiveCoverageIndex(channels, statuses []string) liveCoverageIndex {
	index := liveCoverageIndex{
		channels: make(map[string]struct{}, len(channels)),
		statuses: make(map[string]struct{}, len(statuses)),
	}
	for _, channelID := range channels {
		index.channels[channelID] = struct{}{}
	}

	for _, status := range statuses {
		index.statuses[status] = struct{}{}
	}

	return index
}

func (i liveCoverageIndex) covers(channelID, status string) bool {
	if channelID == "" {
		return false
	}

	if _, ok := i.channels[channelID]; !ok {
		return false
	}

	if len(i.statuses) == 0 {
		return true
	}

	_, ok := i.statuses[status]

	return ok
}

const liveCoverageLinearReadBudget = 32

type liveCoverageMatcher struct {
	channels    []string
	statuses    []string
	linearReads int
	index       liveCoverageIndex
}

func newLiveCoverageMatcher(channels, statuses []string) liveCoverageMatcher {
	return liveCoverageMatcher{channels: channels, statuses: statuses}
}

func (m *liveCoverageMatcher) covers(channelID, status string) bool {
	if m.index.channels != nil {
		return m.index.covers(channelID, status)
	}

	if channelID == "" {
		return false
	}

	if len(m.channels) <= 8 && len(m.statuses) <= 8 {
		return m.coversLinearly(channelID, status)
	}

	if m.linearReads < liveCoverageLinearReadBudget {
		m.linearReads++
		return m.coversLinearly(channelID, status)
	}

	m.index = newLiveCoverageIndex(m.channels, m.statuses)
	m.channels, m.statuses = nil, nil

	return m.index.covers(channelID, status)
}

func (m *liveCoverageMatcher) coversLinearly(channelID, status string) bool {
	return slices.Contains(m.channels, channelID) &&
		(len(m.statuses) == 0 || slices.Contains(m.statuses, status))
}

func (s *reduceSession) absenceCovers(slot *AbsenceSlot, existing *SessionState) bool {
	if s.absenceCoverageSlot != slot {
		s.absenceCoverage = newLiveCoverageMatcher(slot.Coverage.RequestedChannelIDs, slot.Coverage.Filters.Statuses)
		s.absenceCoverageSlot = slot
	}

	return s.absenceCoverage.covers(existing.ChannelID, string(existing.Status))
}
