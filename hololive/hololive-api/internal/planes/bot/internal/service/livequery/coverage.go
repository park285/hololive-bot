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

// snapshot.sql이 집계한 여섯 사실을 비트로 전달해 최대 10,000채널의 JSON 크기를 제한합니다.
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
