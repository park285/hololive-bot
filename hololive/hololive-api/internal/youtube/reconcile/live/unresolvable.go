package live

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func anyPositiveAtOrAfter(clock *LiveEvidenceClock, at time.Time) bool {
	for _, positive := range []*time.Time{clock.LastUpcomingPositiveAt, clock.LastLivePositiveAt} {
		if positive != nil && !positive.Before(at) {
			return true
		}
	}

	return false
}

func clearUnresolvableTracking(clock *LiveEvidenceClock, observedAt time.Time) {
	if clock.UnresolvableSince != nil && !observedAt.Before(*clock.UnresolvableSince) {
		clock.UnresolvableSince = nil
	}
}

func UnresolvableTrackable(existing *SessionState) bool {
	switch existing.Status {
	case domain.LiveStatusLive:
		return existing.Clock.LastLivePositiveAt != nil && existing.Clock.LastLivePositiveSeenAt != nil
	case domain.LiveStatusUpcoming:
		return existing.HeadPresent || existing.LifecycleOrigin == OriginObserved
	case domain.LiveStatusEnded:
		return false
	default:
		return false
	}
}

func applyUnresolvableVideo(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if !ok || !existing.Present || fact.ChannelID != existing.ChannelID || !UnresolvableTrackable(&existing) {
		recordApplication(session, fact.VideoID, "UNRESOLVABLE_IGNORED")

		return
	}

	if anyPositiveAtOrAfter(&existing.Clock, session.evidence.EffectiveAt) {
		recordApplication(session, fact.VideoID, "NEWER_POSITIVE_RETAINED")

		return
	}

	since := existing.Clock.UnresolvableSince
	if since == nil {
		existing.Clock.UnresolvableSince = copyTime(session.evidence.EffectiveAt)
		storeSessionDecision(session, &existing, "UNRESOLVABLE_TRACKED")

		return
	}

	if existing.Status != domain.LiveStatusLive || !fact.VerifiedTerminal ||
		session.dbNow.Before(existing.Clock.LastLivePositiveSeenAt.Add(session.grace)) {
		recordApplication(session, fact.VideoID, "UNRESOLVABLE_RETAINED")

		return
	}

	terminateSession(&existing, *since, session.evidence.EffectiveAt, EndReasonUnresolvableVideo, session.dbNow)
	storeSessionDecision(session, &existing, "ENDED")
	delete(session.state.PendingEnds, fact.VideoID)
}
