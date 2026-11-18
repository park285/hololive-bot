package livequery

const (
	projectionValid coverageFacts = 1 << iota
	targetsCollected
	inconsistentState
	pendingEnd
	futureClock
	staleLive
)

type coverageFacts uint8

type channelSnapshot struct {
	Channel

	Facts coverageFacts `json:"facts"`
}

func (s channelSnapshot) channel() Channel {
	switch {
	case s.Facts&projectionValid == 0:
		s.Reason = InvalidProjection
	case s.Facts&targetsCollected == 0:
		s.Reason = Uncollected
	case s.Facts&inconsistentState != 0:
		s.Reason = Inconsistent
	case s.Facts&pendingEnd != 0:
		s.Reason = ConfirmingEnd
	case s.Facts&futureClock != 0:
		s.Reason = InvalidClock
	case s.Facts&staleLive != 0:
		s.Reason = Stale
	case s.CoveredAt == nil:
		s.Reason = Incomplete
	default:
		s.Reason = Covered
	}

	return s.Channel
}
