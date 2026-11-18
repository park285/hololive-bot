package live

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TerminalEndBlockedByPositive(clock *LiveEvidenceClock, endedAt, dbNow time.Time, grace time.Duration) bool {
	positives := [...]struct{ at, seen *time.Time }{
		{clock.LastUpcomingPositiveAt, clock.LastUpcomingPositiveSeenAt},
		{clock.LastLivePositiveAt, clock.LastLivePositiveSeenAt},
	}

	for _, positive := range positives {
		if positive.at == nil || positive.at.Before(endedAt) {
			continue
		}

		if positive.seen == nil || dbNow.Before(positive.seen.Add(grace)) {
			return true
		}
	}

	return false
}

func applyVerifiedVideoEnd(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if !ok || !existing.Present || existing.Status == domain.LiveStatusEnded ||
		fact.ChannelID != existing.ChannelID || fact.EndedAt == nil || fact.EndedAt.IsZero() {
		return
	}

	if anyPositiveAtOrAfter(&existing.Clock, session.evidence.EffectiveAt) {
		return
	}

	if TerminalEndBlockedByPositive(&existing.Clock, *fact.EndedAt, session.dbNow, session.grace) {
		return
	}

	if pending, exists := session.state.PendingEnds[fact.VideoID]; exists && pending.EffectiveAt.After(session.evidence.EffectiveAt) {
		return
	}

	pending := pendingFromFact(session, fact, EndEvidenceExplicitEnd)
	endSession(&existing, &pending, session.dbNow)
	storeSessionDecision(session, &existing, "ENDED")
	delete(session.state.PendingEnds, fact.VideoID)
}
