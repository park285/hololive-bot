package live

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func BenchmarkLiveIgnoredAbsenceHistory(b *testing.B) {
	const count = 10000

	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	initial := liveEvidence(1, base, contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("UPCOMING"))
	fact := sessionFact("UPCOMING")
	session := newSession(&fact, StatusUpcoming, &initial)
	state := State{Sessions: map[string]SessionState{}, PendingEnds: map[string]PendingEnd{}}

	for i := range count {
		at := base.Add(time.Duration(i+1) * time.Minute)

		session.IgnoredAbsenceScheduledFor = append(session.IgnoredAbsenceScheduledFor, at)
		state.AbsenceSlots = append(state.AbsenceSlots, AbsenceSlot{ObservationID: int64(i + 2), ScheduledFor: at, EffectiveAt: at, ReceivedAt: at, Coverage: channelCoverage()})
	}

	state.Sessions[session.VideoID] = session

	evidence := liveEvidence(count+2, base.Add((count+1)*time.Minute), contract.CompletenessComplete, contract.ContinuityContiguous)

	b.ReportAllocs()

	for b.Loop() {
		decision, err := Reduce(state, evidence, time.Hour, evidence.ReceivedAt)
		if err != nil {
			b.Fatal(err)
		}

		if len(decision.Sessions) != 1 || len(decision.Sessions[0].IgnoredAbsenceScheduledFor) != count+1 {
			b.Fatal("ignored absence replay changed the domain state")
		}
	}
}
