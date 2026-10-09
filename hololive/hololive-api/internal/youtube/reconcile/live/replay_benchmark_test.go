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

	// 운영과 같은 observed UPCOMING positive 세션에서 시작한다. newSession의 metadata_only
	// 세션은 부재 적용 전에 걸러져 이력 재생 비용을 측정하지 못한다.
	created, err := Reduce(State{}, initial, time.Hour, initial.ReceivedAt)
	if err != nil {
		b.Fatal(err)
	}

	if len(created.Sessions) != 1 {
		b.Fatal("initial UPCOMING positive did not create a tracked session")
	}

	session := created.Sessions[0]
	state := State{Sessions: map[string]SessionState{}, PendingEnds: map[string]PendingEnd{}}
	history := make([]time.Time, 0, count)

	for i := range count {
		at := base.Add(time.Duration(i+1) * time.Minute)

		history = append(history, at)
		state.AbsenceSlots = append(state.AbsenceSlots, AbsenceSlot{ObservationID: int64(i + 2), ScheduledFor: at, EffectiveAt: at, ReceivedAt: at, Coverage: channelCoverage()})
	}

	session.IgnoredAbsences = LoadedIgnoredAbsences(history)
	state.Sessions[session.VideoID] = session

	evidence := liveEvidence(count+2, base.Add((count+1)*time.Minute), contract.CompletenessComplete, contract.ContinuityContiguous)

	b.ReportAllocs()

	for b.Loop() {
		decision, err := Reduce(state, evidence, time.Hour, evidence.ReceivedAt)
		if err != nil {
			b.Fatal(err)
		}

		if len(decision.Sessions) != 1 {
			b.Fatal("ignored absence replay changed the domain state")
		}

		if slots, loaded := decision.Sessions[0].IgnoredAbsences.Slots(); !loaded || len(slots) != count+1 {
			b.Fatal("ignored absence replay changed the domain state")
		}
	}
}
