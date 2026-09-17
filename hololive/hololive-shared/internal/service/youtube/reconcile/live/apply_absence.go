package live

import (
	"slices"
	"time"
)

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
	if existing.Status == StatusEnded || !existing.Present {
		return
	}

	covers := session.absenceCovers(slot, existing)
	if !covers {
		return
	}

	ignoredSlots := ignoredAbsenceSlots(session, existing)
	if _, ignored := ignoredSlots[slot.ScheduledFor.UTC()]; ignored {
		return
	}

	if existing.Clock.LastLivePositiveAt == nil {
		existing.IgnoredAbsenceScheduledFor = append(existing.IgnoredAbsenceScheduledFor, slot.ScheduledFor)
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

func ignoredAbsenceSlots(session *reduceSession, existing *SessionState) map[time.Time]struct{} {
	if session.ignoredAbsences == nil {
		session.ignoredAbsences = make(map[string]map[time.Time]struct{})
	}

	ignored := session.ignoredAbsences[existing.VideoID]
	if ignored == nil {
		// 신규 positive가 긴 이력을 복원할 때 같은 목록을 slot마다 선형 탐색하지 않는다.
		ignored = make(map[time.Time]struct{}, len(existing.IgnoredAbsenceScheduledFor))
		for _, at := range existing.IgnoredAbsenceScheduledFor {
			ignored[at.UTC()] = struct{}{}
		}

		session.ignoredAbsences[existing.VideoID] = ignored
	}

	return ignored
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

// 한 reducer 호출에서 같은 coverage를 세션마다 다시 선형 탐색하지 않는다.
// 빈 statuses는 모든 상태를 허용하지만 빈 channels는 아무 채널도 허용하지 않는다.
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

// coverage는 reducer가 복제한 불변 상태에서 빌린다. 작은 작업에 해시맵 할당을
// 강제하지 않고, 선형 탐색 횟수를 고정 상한으로 제한한 뒤 인덱스로 전환한다.
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
	// 부재 적용은 slot을 바깥 루프에서 순회한다. 현재 slot만 유지하여
	// 긴 이력 전체에 대한 O(H*C) 보조 메모리를 만들지 않는다.
	if s.absenceCoverageSlot != slot {
		s.absenceCoverage = newLiveCoverageMatcher(slot.Coverage.RequestedChannelIDs, slot.Coverage.Filters.Statuses)
		s.absenceCoverageSlot = slot
	}
	return s.absenceCoverage.covers(existing.ChannelID, string(existing.Status))
}
